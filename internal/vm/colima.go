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

// GetPublishedPort returns containerPort unchanged. Colima uses an SSH tunnel
// so the host port always matches the container port; there is no Docker-style
// auto-assignment.
func (c *ColimaVM) GetPublishedPort(containerPort int) (int, error) { return containerPort, nil }
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
		os.Remove(filepath.Join(c.stateDir, VMCreatedAtFile))
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

func (c *ColimaVM) SSH(ctx context.Context, env map[string]string) error {
	if len(env) == 0 {
		return run.Interactive(ctx, "colima", "ssh", "--profile", c.profile)
	}
	return InteractiveSSH(ctx, c.profile, env, "exec bash -l")
}

func (c *ColimaVM) RunStream(ctx context.Context, script string, env map[string]string) (int, error) {
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

	sshConfig, sshHost := colimaSSHEndpoint(c.profile)
	args := []string{"-F", sshConfig}
	args = append(args, OpenSSHOptions()...)
	args = append(args, sshHost, "bash", "-lc", bashScript)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	flush := attachProcessStderr(cmd)
	defer flush()
	code, err := ExitCodeFromError(cmd.Run())
	return code, err
}

func (c *ColimaVM) RunInteractive(ctx context.Context, script string, env map[string]string) error {
	return InteractiveSSH(ctx, c.profile, env, script)
}

// CopyTo copies a file or directory from the host at localPath into the VM at
// vmPath using scp. Pass recursive=true for directory trees.
func (c *ColimaVM) CopyTo(ctx context.Context, localPath, vmPath string, recursive bool) error {
	sshConfig, sshHost := colimaSSHEndpoint(c.profile)
	args := []string{"-F", sshConfig}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, localPath, sshHost+":"+vmPath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyFrom copies a file or directory from the VM at vmPath to the host at
// localPath using scp. Pass recursive=true for directory trees.
func (c *ColimaVM) CopyFrom(ctx context.Context, vmPath, localPath string, recursive bool) error {
	sshConfig, sshHost := colimaSSHEndpoint(c.profile)
	args := []string{"-F", sshConfig}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, sshHost+":"+vmPath, localPath)
	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
	return filepath.Join(c.stateDir, VMCreatedAtFile)
}
