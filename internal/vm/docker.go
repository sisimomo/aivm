package vm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const dockerContainerUser = "user"

// DockerVM is a VM implementation backed by a Docker container.
// Each profile maps to a single long-lived container whose lifecycle mirrors
// the vm.VM interface. Scripts execute via docker exec, so bootstrap scripts
// run in a real Linux environment.
type DockerVM struct {
	mu            sync.Mutex
	profile       string
	stateDir      string
	image         string
	containerName string
	lastStartOpts StartOptions
}

var _ VM = (*DockerVM)(nil)

// NewDocker returns a DockerVM for the given profile, state directory, and
// base image. The container is not started — call Start to create or resume it.
func NewDocker(profile, stateDir, image string) *DockerVM {
	return &DockerVM{
		profile:       profile,
		stateDir:      stateDir,
		image:         image,
		containerName: profile,
	}
}

func (d *DockerVM) Profile() string              { return d.profile }
func (d *DockerVM) NeedsPortBindingAtBoot() bool { return true }

// Status reports whether the container exists and its current state.
func (d *DockerVM) Status(ctx context.Context) (Status, error) {
	out, err := dockerOutput(ctx, "inspect", "--format", "{{.State.Status}}", d.containerName)
	if err != nil {
		return StatusNotFound, nil
	}
	switch strings.TrimSpace(out) {
	case "running":
		return StatusRunning, nil
	case "exited", "stopped", "paused", "created":
		return StatusStopped, nil
	default:
		return StatusNotFound, nil
	}
}

// Start creates and starts the container. If already running it is a no-op; if
// stopped, it is restarted (mounts and port bindings were baked in at creation
// and are preserved by Docker on restart). A new container is created with the
// configured image, mounts, and port forwards.
func (d *DockerVM) Start(ctx context.Context, opts StartOptions) error {
	status, _ := d.Status(ctx)

	switch status {
	case StatusRunning:
		return nil

	case StatusStopped:
		return dockerCmd(ctx, "start", d.containerName)

	default:
		return d.startFromImage(ctx, d.image, opts)
	}
}

// startFromImage creates and starts a new container from the given image with
// mounts and port mappings from opts.
func (d *DockerVM) startFromImage(ctx context.Context, image string, opts StartOptions) error {
	d.mu.Lock()
	d.lastStartOpts = opts
	d.mu.Unlock()

	args := []string{"run", "-d", "--name", d.containerName}
	if opts.Privileged {
		args = append(args, "--privileged")
	}
	for _, pm := range opts.PortMappings {
		args = append(args, "-p", fmt.Sprintf("%d:%d", pm.HostPort, pm.ContainerPort))
	}
	for _, m := range opts.Mounts {
		mode := "ro"
		if m.Writable {
			mode = "rw"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.HostPath, m.HostPath, mode))
	}
	args = append(args, image)
	return dockerCmd(ctx, args...)
}

// Stop stops the running container without removing it.
func (d *DockerVM) Stop(ctx context.Context) error {
	status, _ := d.Status(ctx)
	if status != StatusRunning {
		return nil
	}
	return dockerCmd(ctx, "stop", d.containerName)
}

// Destroy stops and removes the container.
func (d *DockerVM) Destroy(ctx context.Context) error {
	_ = dockerCmd(ctx, "stop", d.containerName)
	_ = dockerCmd(ctx, "rm", "-f", d.containerName)
	os.Remove(filepath.Join(d.stateDir, VMCreatedAtFile))
	return nil
}

// DestroyWithImages removes the container. Use this for full cleanup (e.g. test teardown).
func (d *DockerVM) DestroyWithImages() {
	ctx := context.Background()
	_ = dockerCmd(ctx, "stop", d.containerName)
	_ = dockerCmd(ctx, "rm", "-f", d.containerName)
	os.Remove(filepath.Join(d.stateDir, VMCreatedAtFile))
}

// Run executes script inside the container as the container user.
func (d *DockerVM) Run(ctx context.Context, script string, env map[string]string) error {
	return dockerCmd(
		ctx,
		"exec",
		"-u", dockerContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env, true),
	)
}

// RunOutput executes script inside the container and returns combined stdout.
func (d *DockerVM) RunOutput(ctx context.Context, script string, env map[string]string) (string, error) {
	return dockerOutput(
		ctx,
		"exec",
		"-u", dockerContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env, true),
	)
}

// RunStream executes script without a PTY, streaming stdout/stderr to the host.
func (d *DockerVM) RunStream(ctx context.Context, script string, env map[string]string) (int, error) {
	args := []string{
		"exec", "-i",
		"-u", dockerContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env, false),
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return ExitCodeFromError(cmd.Run())
}

// RunInteractive executes script with a PTY attached when available, suitable
// for TUI applications (e.g. agent CLIs). stdin/stdout/stderr are connected to
// the calling process. The -t flag is only passed when stdin is a TTY so that
// the command also works in non-interactive environments (tests, CI).
func (d *DockerVM) RunInteractive(ctx context.Context, script string, env map[string]string) error {
	args := []string{"exec", "-i"}
	if isTTY() {
		args = append(args, "-t")
	}
	args = append(args, "-u", dockerContainerUser, d.containerName, "bash", "-lc", buildBashCmd(script, env, false))
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SSH opens an interactive shell in the container. Session env is exported first;
// -t is only passed when stdin is a TTY.
func (d *DockerVM) SSH(ctx context.Context, env map[string]string) error {
	args := []string{"exec", "-i"}
	if isTTY() {
		args = append(args, "-t")
	}
	args = append(args, "-u", dockerContainerUser, d.containerName, "bash", "-c", BuildDockerSSHCmd(env))
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyTo copies a file or directory from the host at localPath into the
// container at vmPath. docker cp handles directory trees natively; the
// recursive flag is accepted for interface compatibility but has no effect.
func (d *DockerVM) CopyTo(ctx context.Context, localPath, vmPath string, _ bool) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", localPath, d.containerName+":"+vmPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CopyFrom copies a file or directory from the container at vmPath to the host
// at localPath. docker cp handles directory trees natively; the recursive flag
// is accepted for interface compatibility but has no effect.
func (d *DockerVM) CopyFrom(ctx context.Context, vmPath, localPath string, _ bool) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", d.containerName+":"+vmPath, localPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// WaitReady polls until the container responds to a simple command.
func (d *DockerVM) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("container %s did not become ready within %s", d.containerName, timeout)
			}
			if err := dockerCmd(ctx, "exec", "-u", dockerContainerUser, d.containerName, "echo", "ready"); err == nil {
				return nil
			}
		}
	}
}

// GetPublishedPort retrieves the host port that Docker assigned for the given
// container port. Returns 0 if the port is not published or the container is not running.
// This is used by the test harness to discover auto-assigned ports after container creation.
func (d *DockerVM) GetPublishedPort(containerPort int) (int, error) {
	// Query Docker for the port mapping: {{(index (index .NetworkSettings.Ports "3773/tcp") 0).HostPort}}
	template := fmt.Sprintf("{{(index (index .NetworkSettings.Ports \"%d/tcp\") 0).HostPort}}", containerPort)

	out, err := dockerOutput(context.Background(), "inspect", "--format", template, d.containerName)
	if err != nil {
		return 0, err
	}

	hostPort, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("failed to parse published port %q: %w", strings.TrimSpace(out), err)
	}

	return hostPort, nil
}

// ── helpers ────────────────────────────────────────────────────────────────

// BuildDockerSSHCmd builds the bash -c script for Docker SSH (export env, then exec bash).
func BuildDockerSSHCmd(env map[string]string) string {
	var sb strings.Builder
	for k, v := range env {
		fmt.Fprintf(&sb, "export %s=%s; ", k, ShellEscape(v))
	}
	sb.WriteString("exec bash")
	return sb.String()
}

// buildBashCmd returns a bash -lc command string that executes script inside
// the container. The script is base64-encoded and written to a temp file before
// execution to avoid stdin consumption by package managers (dpkg, apt) during
// bootstrap runs. When combineStderr is true, stderr is merged into stdout for capture.
func buildBashCmd(script string, env map[string]string, combineStderr bool) string {
	full := script
	if len(env) > 0 {
		var sb strings.Builder
		for k, v := range env {
			fmt.Fprintf(&sb, "export %s=%s\n", k, ShellEscape(v))
		}
		full = sb.String() + script
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(full))
	stderrPart := ""
	if combineStderr {
		stderrPart = " 2>&1"
	}
	return "t=$(mktemp) && echo " + encoded + " | base64 -d > \"$t\" && bash -l \"$t\"" + stderrPart + "; ec=$?; rm -f \"$t\"; exit $ec"
}

// dockerCmd runs a docker command, discarding stdout.
func dockerCmd(ctx context.Context, args ...string) error {
	_, err := dockerOutput(ctx, args...)
	return err
}

// dockerOutput runs a docker command and returns combined stdout, or an error
// that includes stderr for debugging.
func dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// IsTTY reports whether stdin is connected to a real terminal.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func isTTY() bool { return IsTTY() }
