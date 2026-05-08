package vm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
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

func (d *DockerVM) Profile() string             { return d.profile }
func (d *DockerVM) NeedsPortBindingAtBoot() bool { return true }

// Status reports whether the container exists and its current state.
func (d *DockerVM) Status(_ context.Context) (Status, error) {
	out, err := dockerOutput("inspect", "--format", "{{.State.Status}}", d.containerName)
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
func (d *DockerVM) Start(_ context.Context, opts StartOptions) error {
	status, _ := d.Status(context.Background())

	switch status {
	case StatusRunning:
		return nil

	case StatusStopped:
		return dockerCmd("start", d.containerName)

	default:
		d.mu.Lock()
		d.lastStartOpts = opts
		d.mu.Unlock()

		args := []string{"run", "-d", "--name", d.containerName}
		for _, port := range opts.PortForwards {
			args = append(args, "-p", fmt.Sprintf("%d:%d", port, port))
		}
		for _, m := range opts.Mounts {
			mode := "ro"
			if m.Writable {
				mode = "rw"
			}
			args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.HostPath, m.HostPath, mode))
		}
		args = append(args, d.image)
		return dockerCmd(args...)
	}
}

// Stop stops the running container without removing it.
func (d *DockerVM) Stop(_ context.Context) error {
	status, _ := d.Status(context.Background())
	if status != StatusRunning {
		return nil
	}
	return dockerCmd("stop", d.containerName)
}

// Destroy stops and removes the container. Snapshot images are preserved so
// that TryRestoreBaseImage can find them after recreation. Images are removed
// only by DestroyWithImages, which is called during test teardown.
func (d *DockerVM) Destroy(_ context.Context) error {
	_ = dockerCmd("stop", d.containerName)
	_ = dockerCmd("rm", "-f", d.containerName)
	return nil
}

// DestroyWithImages removes the container and all snapshot images created for
// this profile. Use this for full cleanup (e.g. test teardown).
func (d *DockerVM) DestroyWithImages() {
	_ = dockerCmd("stop", d.containerName)
	_ = dockerCmd("rm", "-f", d.containerName)

	prefix := fmt.Sprintf("aivm-snap-%s-", d.profile)
	out, _ := dockerOutput("images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference="+prefix+"*")
	for _, ref := range splitLines(out) {
		if ref != "" {
			_ = dockerCmd("rmi", "-f", ref)
		}
	}
}

// Run executes script inside the container as the container user.
func (d *DockerVM) Run(_ context.Context, script string, env map[string]string) error {
	return dockerCmd(
		"exec",
		"-u", dockerContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env),
	)
}

// RunOutput executes script inside the container and returns combined stdout.
func (d *DockerVM) RunOutput(_ context.Context, script string, env map[string]string) (string, error) {
	return dockerOutput(
		"exec",
		"-u", dockerContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env),
	)
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
	args = append(args, "-u", dockerContainerUser, d.containerName, "bash", "-lc", buildBashCmd(script, env))
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// SSH opens an interactive shell session inside the container.
// When stdin is not a TTY (e.g. in automated tests or CI), the -t flag is
// omitted so that docker exec does not require a pseudo-terminal.
func (d *DockerVM) SSH(ctx context.Context) error {
	args := []string{"exec", "-i"}
	if isTTY() {
		args = append(args, "-t")
	}
	args = append(args, "-u", dockerContainerUser, d.containerName, "bash")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// WaitReady polls until the container responds to a simple command.
func (d *DockerVM) WaitReady(_ context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := dockerCmd("exec", "-u", dockerContainerUser, d.containerName, "echo", "ready"); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("container %s did not become ready within %s", d.containerName, timeout)
}

// CreateSnapshot commits the current container filesystem as a Docker image tag.
func (d *DockerVM) CreateSnapshot(_ context.Context, name string) error {
	tag := d.snapshotTag(name)
	if err := dockerCmd("commit", d.containerName, tag); err != nil {
		return fmt.Errorf("create snapshot %q: %w", name, err)
	}
	return nil
}

// RestoreSnapshot recreates the container from a previously committed snapshot
// image, re-applying the original start options (mounts and port bindings).
// Returns (false, nil) when the snapshot does not exist.
func (d *DockerVM) RestoreSnapshot(_ context.Context, name string) (bool, error) {
	tag := d.snapshotTag(name)
	if _, err := dockerOutput("inspect", "--type", "image", tag); err != nil {
		return false, nil
	}

	_ = dockerCmd("stop", d.containerName)
	_ = dockerCmd("rm", "-f", d.containerName)

	d.mu.Lock()
	opts := d.lastStartOpts
	d.mu.Unlock()

	args := []string{"run", "-d", "--name", d.containerName}
	for _, port := range opts.PortForwards {
		args = append(args, "-p", fmt.Sprintf("%d:%d", port, port))
	}
	for _, m := range opts.Mounts {
		mode := "ro"
		if m.Writable {
			mode = "rw"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.HostPath, m.HostPath, mode))
	}
	args = append(args, tag)

	if err := dockerCmd(args...); err != nil {
		return false, fmt.Errorf("restore snapshot %q: %w", name, err)
	}
	return true, nil
}

// ListSnapshots queries Docker for snapshot images created for this profile.
func (d *DockerVM) ListSnapshots(_ context.Context) ([]Snapshot, error) {
	prefix := fmt.Sprintf("aivm-snap-%s-", d.profile)
	out, err := dockerOutput("images", "--format", "{{.Repository}}:{{.Tag}}", "--filter", "reference="+prefix+"*")
	if err != nil {
		return nil, nil
	}
	var snaps []Snapshot
	for _, ref := range splitLines(out) {
		if ref == "" {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(ref, prefix), ":latest")
		snaps = append(snaps, Snapshot{Name: name})
	}
	return snaps, nil
}

func (d *DockerVM) snapshotTag(name string) string {
	safe := strings.NewReplacer(" ", "-", "/", "-", ":", "-").Replace(name)
	return fmt.Sprintf("aivm-snap-%s-%s:latest", d.profile, safe)
}

// ── helpers ────────────────────────────────────────────────────────────────

// buildBashCmd returns a bash -lc command string that executes script inside
// the container. The script is base64-encoded and written to a temp file before
// execution to avoid stdin consumption by package managers (dpkg, apt) during
// bootstrap runs.
func buildBashCmd(script string, env map[string]string) string {
	full := script
	if len(env) > 0 {
		var sb strings.Builder
		for k, v := range env {
			fmt.Fprintf(&sb, "export %s=%s\n", k, ShellEscape(v))
		}
		full = sb.String() + script
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(full))
	return "t=$(mktemp) && echo " + encoded + " | base64 -d > \"$t\" && bash -l \"$t\"; ec=$?; rm -f \"$t\"; exit $ec"
}

// dockerCmd runs a docker command, discarding stdout.
func dockerCmd(args ...string) error {
	_, err := dockerOutput(args...)
	return err
}

// dockerOutput runs a docker command and returns combined stdout, or an error
// that includes stderr for debugging.
func dockerOutput(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// isTTY reports whether stdin is connected to a real terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// splitLines splits s on newlines, trimming carriage returns.
func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r\n", "\n"), "\n")
}
