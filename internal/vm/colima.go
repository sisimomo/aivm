package vm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/run"
)

type ColimaVM struct {
	profile  string
	stateDir string
	lock     *LifecycleLock
}

func NewColima(profile, stateDir string) *ColimaVM {
	return &ColimaVM{
		profile:  profile,
		stateDir: stateDir,
		lock:     NewLifecycleLock(stateDir),
	}
}

func (c *ColimaVM) Profile() string              { return c.profile }
func (c *ColimaVM) NeedsPortBindingAtBoot() bool { return false }

func (c *ColimaVM) Status(ctx context.Context) (Status, error) {
	lines, err := run.OutputLines(ctx, "colima", "list")
	if err != nil {
		return StatusNotFound, nil
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == c.profile {
			switch fields[1] {
			case "Running":
				return StatusRunning, nil
			case "Stopped":
				return StatusStopped, nil
			}
		}
	}
	return StatusNotFound, nil
}

func (c *ColimaVM) Start(ctx context.Context, opts StartOptions) error {
	release, err := c.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := c.Status(ctx)
	if err != nil {
		return err
	}

	logDir := filepath.Join(c.stateDir, "logs")
	_ = os.MkdirAll(logDir, 0755)
	logFile, _ := os.OpenFile(filepath.Join(logDir, "colima.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logFile != nil {
		defer logFile.Close()
	}
	w := aivmlog.Writer("colima")

	switch status {
	case StatusRunning:
		aivmlog.Info("VM '%s' is already running", c.profile)
		return nil

	case StatusStopped:
		aivmlog.Step("Resuming stopped VM '%s'", c.profile)
		cmd := exec.CommandContext(ctx, "colima", "start", c.profile)
		cmd.Stdout = w
		cmd.Stderr = w
		return cmd.Run()

	default:
		aivmlog.Step("Creating Colima VM '%s'", c.profile)
		aivmlog.Info("CPU=%d Memory=%dGiB Disk=%dGiB Type=%s",
			opts.CPUs, opts.MemoryBytes>>30, opts.DiskBytes>>30, opts.VMType)

		vmTypeFlags := c.vmTypeFlags(opts.VMType)

		args := []string{
			"start", c.profile,
			"--cpu", strconv.Itoa(opts.CPUs),
			"--memory", strconv.Itoa(int(opts.MemoryBytes >> 30)),
			"--disk", strconv.Itoa(int(opts.DiskBytes >> 30)),
		}
		args = append(args, vmTypeFlags...)

		for _, m := range opts.Mounts {
			flag := m.HostPath + ":r"
			if m.Writable {
				flag = m.HostPath + ":w"
			}
			args = append(args, "--mount", flag)
		}
		if !opts.SSHAgent {
			args = append(args, "--ssh-agent=false")
		}

		cmd := exec.CommandContext(ctx, "colima", args...)
		cmd.Stdout = w
		cmd.Stderr = w
		return cmd.Run()
	}
}

func (c *ColimaVM) Stop(ctx context.Context) error {
	release, err := c.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := c.Status(ctx)
	if err != nil || status != StatusRunning {
		aivmlog.Info("VM '%s' is not running — nothing to stop", c.profile)
		return nil
	}

	aivmlog.Info("Stopping Docker containers inside VM...")
	_ = c.Run(ctx, "docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true", nil)

	aivmlog.Step("Stopping Colima VM '%s'", c.profile)
	w := aivmlog.Writer("colima")
	cmd := exec.CommandContext(ctx, "colima", "stop", c.profile)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		aivmlog.Warn("graceful stop failed, forcing...")
		_ = run.Quiet(ctx, "colima", "stop", c.profile, "--force")
	}
	aivmlog.Success("VM '%s' stopped (disk preserved)", c.profile)
	return nil
}

func (c *ColimaVM) Destroy(ctx context.Context) error {
	release, err := c.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if status == StatusRunning {
		_ = c.Run(ctx, "docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true", nil)
		_ = run.Quiet(ctx, "colima", "stop", c.profile, "--force")
	}

	if status != StatusNotFound {
		aivmlog.Step("Deleting VM profile '%s'", c.profile)
		w := aivmlog.Writer("colima")
		cmd := exec.CommandContext(ctx, "colima", "delete", c.profile, "--force", "--data")
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("delete VM: %w", err)
		}
		aivmlog.Success("VM '%s' destroyed", c.profile)
		os.Remove(filepath.Join(c.stateDir, "vm-created-at"))
	} else {
		aivmlog.Info("VM '%s' does not exist — nothing to destroy", c.profile)
	}
	return nil
}

func (c *ColimaVM) Run(ctx context.Context, script string, env map[string]string) error {
	full := script
	if len(env) > 0 {
		var sb strings.Builder
		for k, v := range env {
			fmt.Fprintf(&sb, "export %s=%s\n", k, shellescape(v))
		}
		full = sb.String() + script
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(full))
	// Pass as separate args so colima/SSH runs them as distinct tokens.
	// A single-string argument is passed verbatim by SSH exec without shell
	// interpretation (pipes become literal arguments to the first command).
	bashScript := "echo " + encoded + " | base64 -d | bash -l"

	w := aivmlog.Writer("vm")
	cmd := exec.CommandContext(ctx, "colima", "ssh", "--profile", c.profile, "--", "bash", "-lc", bashScript)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// RunOutput executes a script inside the VM and returns its combined stdout+stderr.
func (c *ColimaVM) RunOutput(ctx context.Context, script string, env map[string]string) (string, error) {
	full := script
	if len(env) > 0 {
		var sb strings.Builder
		for k, v := range env {
			fmt.Fprintf(&sb, "export %s=%s\n", k, shellescape(v))
		}
		full = sb.String() + script
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(full))
	bashScript := "echo " + encoded + " | base64 -d | bash -l"

	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "colima", "ssh", "--profile", c.profile, "--", "bash", "-lc", bashScript)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run output: %w\n%s", err, buf.String())
	}
	return buf.String(), nil
}

func (c *ColimaVM) SSH(ctx context.Context) error {
	return run.Interactive(ctx, "colima", "ssh", "--profile", c.profile)
}

func (c *ColimaVM) RunInteractive(ctx context.Context, script string, env map[string]string) error {
	return InteractiveSSH(ctx, c.profile, env, script)
}

func (c *ColimaVM) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := run.Quiet(ctx, "colima", "ssh", "--profile", c.profile, "--", "echo", "ready")
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("VM did not become reachable within %s", timeout)
}

func (c *ColimaVM) CreateSnapshot(ctx context.Context, name string) error {
	return run.Quiet(ctx, "colima", "snapshot", "create", "--name", name, c.profile)
}

func (c *ColimaVM) RestoreSnapshot(ctx context.Context, name string) (bool, error) {
	// Disk-file-only restore (no named QEMU snapshot): used on VZ / Apple Silicon
	// where QEMU-style snapshots are unavailable. The staged disk files represent the
	// full bootstrap state. We must stop the currently-running (blank) VM, install the
	// staged disk, and restart so the VM boots from the bootstrap image.
	if name == "" {
		stagingDir := colimaSnapshotStagingDir(c.profile)
		if _, err := os.Stat(stagingDir); os.IsNotExist(err) {
			aivmlog.Debug("no staged disk files for disk-only restore")
			return false, nil
		}
		if err := run.Quiet(ctx, "colima", "stop", c.profile, "--force"); err != nil {
			return false, fmt.Errorf("stopping VM for disk-only restore: %w", err)
		}
		if err := c.applyStagedDiskFiles(ctx); err != nil {
			return false, fmt.Errorf("applying staged disk files: %w", err)
		}
		if err := run.Quiet(ctx, "colima", "start", c.profile); err != nil {
			return false, fmt.Errorf("restarting VM after disk restore: %w", err)
		}
		if err := c.WaitReady(ctx, 90*time.Second); err != nil {
			return false, fmt.Errorf("VM not ready after disk restore: %w", err)
		}
		return true, nil
	}

	// If staged disk files exist (written by TransferSnapshot from a shadow VM),
	// install them into this profile's Lima instance dir before asking Lima/QEMU
	// to restore the named snapshot.  The staging dir is cleaned up on success.
	if err := c.applyStagedDiskFiles(ctx); err != nil {
		aivmlog.Debug("applying staged disk files failed (non-fatal): %v", err)
	}

	snapshots, err := c.ListSnapshots(ctx)
	if err != nil {
		return false, nil
	}
	for _, s := range snapshots {
		if s.Name == name {
			return true, run.Quiet(ctx, "colima", "snapshot", "restore", "--name", name, c.profile)
		}
	}
	return false, nil
}

// applyStagedDiskFiles checks for disk files previously staged by TransferSnapshot
// and, if found, copies them into this profile's Lima instance directory.
// The staging directory is removed on success.
func (c *ColimaVM) applyStagedDiskFiles(ctx context.Context) error {
	stagingDir := colimaSnapshotStagingDir(c.profile)
	if _, err := os.Stat(stagingDir); os.IsNotExist(err) {
		return nil // nothing staged — normal path
	}

	dstDir := colimaLimaInstanceDir(c.profile)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("preparing Lima instance dir: %w", err)
	}

	for _, fname := range []string{"basedisk", "diffdisk"} {
		src := filepath.Join(stagingDir, fname)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // file not staged; skip
		}
		dst := filepath.Join(dstDir, fname)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("installing staged %s: %w", fname, err)
		}
		aivmlog.Info("installed staged disk file: %s → %s", src, dst)
	}

	// Clean up staging dir after successful install.
	_ = os.RemoveAll(stagingDir)
	return nil
}

func (c *ColimaVM) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	lines, err := run.OutputLines(ctx, "colima", "snapshot", "list", c.profile)
	if err != nil {
		return nil, nil
	}
	var snaps []Snapshot
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			snaps = append(snaps, Snapshot{Name: fields[0]})
		}
	}
	return snaps, nil
}

// TransferSnapshot copies the QEMU/VZ disk files that contain the named
// snapshot from this (shadow) VM's Lima instance directory into a staging
// directory inside the target profile's state directory.  The files are
// applied to the live Lima instance during the next RestoreSnapshot call,
// i.e. after the main VM has been destroyed and before it is restarted.
//
// The staging directory path follows the convention understood by
// RestoreSnapshot: <targetStateDir>/snapshot-staging/.
//
// targetStateDir is derived from targetProfile via colimaStateDir.
func (c *ColimaVM) TransferSnapshot(ctx context.Context, _ string, targetProfile string) error {
	srcDir := colimaLimaInstanceDir(c.profile)
	if _, err := os.Stat(srcDir); err != nil {
		return fmt.Errorf("shadow Lima instance dir not found at %s: %w", srcDir, err)
	}

	// Stop the shadow VM so the disk is not in use while we copy it.
	if err := c.Stop(ctx); err != nil {
		return fmt.Errorf("stopping shadow VM before disk transfer: %w", err)
	}

	stagingDir := colimaSnapshotStagingDir(targetProfile)
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("creating snapshot staging dir: %w", err)
	}

	for _, fname := range []string{"basedisk", "diffdisk"} {
		src := filepath.Join(srcDir, fname)
		dst := filepath.Join(stagingDir, fname)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copying %s to staging: %w", fname, err)
		}
	}
	aivmlog.Info("Colima disk files staged for profile '%s' at %s", targetProfile, stagingDir)
	return nil
}

// colimaLimaInstanceDir returns the Lima instance directory for a Colima profile.
// Colima maps profile names to Lima IDs: the default profile ("colima") stays as
// "colima"; any other name becomes "colima-<name>".
// The root is $LIMA_HOME if set, otherwise ~/.lima.
// Colima stores its Lima instances under a "_lima" sub-directory inside $COLIMA_HOME
// (defaulting to ~/.colima/_lima).
func colimaLimaInstanceDir(profile string) string {
	root := colimaLimaHome()
	limaID := "colima"
	if profile != "colima" && profile != "" {
		limaID = "colima-" + profile
	}
	return filepath.Join(root, limaID)
}

// colimaLimaHome returns the directory where Colima stores Lima instance dirs.
// It respects $LIMA_HOME (Lima's own override) and $COLIMA_HOME (Colima's home).
func colimaLimaHome() string {
	if v := os.Getenv("LIMA_HOME"); v != "" {
		return v
	}
	colimaHome := os.Getenv("COLIMA_HOME")
	if colimaHome == "" {
		home, _ := os.UserHomeDir()
		colimaHome = filepath.Join(home, ".colima")
	}
	return filepath.Join(colimaHome, "_lima")
}

// colimaSnapshotStagingDir returns the staging directory where disk files are
// held until the target profile's VM is destroyed and ready to receive them.
// Convention: ~/.colima-aivm-staging/<profile>/ (or $COLIMA_HOME/../colima-aivm-staging/<profile>/).
// We use the user's home dir to keep it outside any single stateDir.
func colimaSnapshotStagingDir(profile string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".colima-aivm-staging", profile)
}

// copyFile copies src to dst preserving sparse regions so that the host file
// only occupies space for data that has actually been written.  Lima disk
// images start life as sparse files (a 60 GiB virtual disk may occupy only a
// few GiB on the host), but a naive byte-by-byte copy materialises every zero
// hole as real disk blocks.  We avoid that by using platform-specific sparse
// copy semantics.
func copyFile(src, dst string) error {
	return sparseCopyFile(src, dst)
}

func (c *ColimaVM) vmTypeFlags(vmType string) []string {
	effective := vmType
	if effective == "" {
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			effective = "vz"
		} else {
			effective = "qemu"
		}
	}
	if effective == "vz" && runtime.GOOS == "darwin" {
		return []string{"--vm-type", "vz", "--vz-rosetta"}
	}
	return []string{"--vm-type", "qemu"}
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (c *ColimaVM) AgeFile() string {
	return filepath.Join(c.stateDir, "vm-created-at")
}
