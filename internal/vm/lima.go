package vm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
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

const stopContainersScript = `command -v docker >/dev/null 2>&1 && \
  docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true`

type LimaVM struct {
	profile  string
	stateDir string
	lock     *LifecycleLock
}

func NewLima(profile, stateDir string) *LimaVM {
	return &LimaVM{
		profile:  profile,
		stateDir: stateDir,
		lock:     NewLifecycleLock(stateDir),
	}
}

func (l *LimaVM) Profile() string              { return l.profile }
func (l *LimaVM) NeedsPortBindingAtBoot() bool { return false }

// GetPublishedPort returns containerPort unchanged. Lima uses an SSH tunnel so
// the host port always matches the container port; there is no Docker-style
// auto-assignment.
func (l *LimaVM) GetPublishedPort(containerPort int) (int, error) { return containerPort, nil }

func (l *LimaVM) Status(ctx context.Context) (Status, error) {
	lines, err := run.OutputLines(ctx, "limactl", "list")
	if err != nil {
		return StatusNotFound, nil
	}
	return ParseLimaListStatus(lines, l.profile), nil
}

func (l *LimaVM) Start(ctx context.Context, opts StartOptions) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := l.Status(ctx)
	if err != nil {
		return err
	}

	switch status {
	case StatusRunning:
		slog.Debug(fmt.Sprintf("VM '%s' is already running", l.profile))
		return nil

	case StatusStopped:
		slog.Info(fmt.Sprintf("Resuming stopped VM '%s'", l.profile))
		cmd := exec.CommandContext(ctx, "limactl", "start", l.profile)
		return aivmlog.RunCmd(cmd, "lima")

	default:
		slog.Info(fmt.Sprintf("Creating Lima VM '%s'", l.profile))
		slog.Debug(fmt.Sprintf("CPU=%d Memory=%dGiB Disk=%dGiB Type=%s",
			opts.CPUs, opts.MemoryBytes>>30, opts.DiskBytes>>30, opts.VMType))

		templatePath, err := LimaTemplatePath()
		if err != nil {
			return err
		}
		defer os.Remove(templatePath)

		args := []string{
			"create", templatePath,
			"--name", l.profile,
			"--cpus", strconv.Itoa(opts.CPUs),
			"--memory", strconv.Itoa(int(opts.MemoryBytes >> 30)),
			"--disk", strconv.Itoa(int(opts.DiskBytes >> 30)),
		}
		args = append(args, l.vmTypeFlags(opts.VMType)...)
		for _, m := range opts.Mounts {
			flag := m.HostPath + ":r"
			if m.Writable {
				flag = m.HostPath + ":w"
			}
			args = append(args, "--mount", flag)
		}
		cmd := exec.CommandContext(ctx, "limactl", args...)
		if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
			return err
		}
		startCmd := exec.CommandContext(ctx, "limactl", "start", l.profile)
		return aivmlog.RunCmd(startCmd, "lima")
	}
}

func (l *LimaVM) Stop(ctx context.Context) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := l.Status(ctx)
	if err != nil || status != StatusRunning {
		slog.Debug(fmt.Sprintf("VM '%s' is not running — nothing to stop", l.profile))
		return nil
	}

	slog.Debug("Stopping Docker containers inside VM...")
	_ = l.Run(ctx, stopContainersScript, nil)

	slog.Info(fmt.Sprintf("Stopping Lima VM '%s'", l.profile))
	cmd := exec.CommandContext(ctx, "limactl", "stop", l.profile)
	if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
		slog.Warn("graceful stop failed, forcing...")
		if forceErr := run.Quiet(ctx, "limactl", "stop", l.profile, "--force"); forceErr != nil {
			return fmt.Errorf("stop VM %q: graceful stop failed: %v; force stop failed: %w", l.profile, err, forceErr)
		}
	}
	slog.Info(fmt.Sprintf("VM '%s' stopped (disk preserved)", l.profile))
	return nil
}

func (l *LimaVM) Destroy(ctx context.Context) error {
	release, err := l.lock.Acquire(30 * time.Second)
	if err != nil {
		return err
	}
	defer release()

	status, err := l.Status(ctx)
	if err != nil {
		return err
	}
	if status == StatusRunning {
		_ = l.Run(ctx, stopContainersScript, nil)
		_ = run.Quiet(ctx, "limactl", "stop", l.profile, "--force")
	}

	if status != StatusNotFound {
		slog.Info(fmt.Sprintf("Deleting VM profile '%s'", l.profile))
		cmd := exec.CommandContext(ctx, "limactl", "delete", l.profile, "--force")
		if err := aivmlog.RunCmd(cmd, "lima"); err != nil {
			return fmt.Errorf("delete VM: %w", err)
		}
		slog.Info(fmt.Sprintf("VM '%s' destroyed", l.profile))
		os.Remove(filepath.Join(l.stateDir, VMCreatedAtFile))
	} else {
		slog.Debug(fmt.Sprintf("VM '%s' does not exist — nothing to destroy", l.profile))
	}
	return nil
}

func (l *LimaVM) Run(ctx context.Context, script string, env map[string]string) error {
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

	cmd := exec.CommandContext(ctx, "limactl", "shell", l.profile, "--", "bash", "-lc", bashScript)
	return aivmlog.RunCmd(cmd, "vm")
}

// RunOutput executes a script inside the VM and returns its combined stdout+stderr.
func (l *LimaVM) RunOutput(ctx context.Context, script string, env map[string]string) (string, error) {
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
	cmd := exec.CommandContext(ctx, "limactl", "shell", l.profile, "--", "bash", "-lc", bashScript)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run output: %w\n%s", err, buf.String())
	}
	return buf.String(), nil
}

func (l *LimaVM) SSH(ctx context.Context, workDir string, env map[string]string) error {
	return InteractiveSSH(ctx, l.profile, env, SSHLoginScript(workDir))
}

func (l *LimaVM) RunStream(ctx context.Context, script string, env map[string]string) (int, error) {
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

	sshConfig, sshHost := LimaSSHEndpoint(l.profile)
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

func (l *LimaVM) RunInteractive(ctx context.Context, script string, env map[string]string) error {
	return InteractiveSSH(ctx, l.profile, env, script)
}

// CopyTo copies a file or directory from the host at localPath into the VM at
// vmPath using scp. Pass recursive=true for directory trees.
func (l *LimaVM) CopyTo(ctx context.Context, localPath, vmPath string, recursive bool) error {
	sshConfig, sshHost := LimaSSHEndpoint(l.profile)
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
func (l *LimaVM) CopyFrom(ctx context.Context, vmPath, localPath string, recursive bool) error {
	sshConfig, sshHost := LimaSSHEndpoint(l.profile)
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

func (l *LimaVM) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := run.Quiet(ctx, "limactl", "shell", l.profile, "--", "echo", "ready")
		if err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("VM did not become reachable within %s", timeout)
}

func (l *LimaVM) vmTypeFlags(vmType string) []string {
	effective := vmType
	if effective == "" {
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			effective = "vz"
		} else {
			effective = "qemu"
		}
	}
	if effective == "vz" && runtime.GOOS == "darwin" {
		return []string{"--vm-type", "vz", "--rosetta"}
	}
	return []string{"--vm-type", "qemu"}
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (l *LimaVM) AgeFile() string {
	return filepath.Join(l.stateDir, VMCreatedAtFile)
}
