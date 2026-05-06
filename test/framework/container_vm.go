package framework

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

const (
	// testImageName is the Docker image used for all integration test containers.
	// Build it with: docker build -t aivm-test-base:latest ./test/docker/
	testImageName = "aivm-test-base:latest"

	// testContainerUser is the non-root user created in the test image.
	testContainerUser = "user"
)

var (
	ensureTestImageOnce sync.Once
	ensureTestImageErr  error
)

// DockerVM is a vm.VM implementation backed by a Docker container.
// Each test gets a dedicated container whose lifecycle maps directly onto the
// vm.VM interface. Scripts execute inside the container via docker exec, which
// means bootstrap scripts run in a real Ubuntu environment and can install
// packages, write files, etc.
//
// DockerVM also implements RunCounter so that existing run-count assertions
// continue to work without any changes to test cases.
type DockerVM struct {
	mu            sync.Mutex
	profile       string
	stateDir      string
	containerName string
	runCount      int
	snapshots     []string // image tags committed as snapshots
}

var _ vm.VM = (*DockerVM)(nil)
var _ RunCounter = (*DockerVM)(nil)

func newDockerVM(profile, stateDir string) *DockerVM {
	return NewDockerVM(profile, stateDir)
}

// NewDockerVM creates a new DockerVM for the given Colima profile and state directory.
// The container is not started — call Start to create and run it.
func NewDockerVM(profile, stateDir string) *DockerVM {
	return &DockerVM{
		profile:       profile,
		stateDir:      stateDir,
		containerName: profile,
	}
}

func (d *DockerVM) Profile() string { return d.profile }

// Status queries Docker to determine whether the container exists and is running.
func (d *DockerVM) Status(_ context.Context) (vm.Status, error) {
	out, err := dockerOutput("inspect", "--format", "{{.State.Status}}", d.containerName)
	if err != nil {
		// docker inspect returns non-zero when the container does not exist.
		return vm.StatusNotFound, nil
	}
	switch strings.TrimSpace(out) {
	case "running":
		return vm.StatusRunning, nil
	case "exited", "stopped", "paused", "created":
		return vm.StatusStopped, nil
	default:
		return vm.StatusNotFound, nil
	}
}

// Start creates and starts the container from the test base image. If the
// container already exists and is stopped, it is restarted. If it is already
// running, Start is a no-op.
func (d *DockerVM) Start(_ context.Context, _ vm.StartOptions) error {
	status, _ := d.Status(context.Background())

	switch status {
	case vm.StatusRunning:
		return nil

	case vm.StatusStopped:
		return dockerRun("start", d.containerName)

	default:
		return dockerRun(
			"run", "-d",
			"--name", d.containerName,
			testImageName,
		)
	}
}

// Stop stops the running container without removing it (disk / filesystem preserved).
func (d *DockerVM) Stop(_ context.Context) error {
	status, _ := d.Status(context.Background())
	if status != vm.StatusRunning {
		return nil
	}
	return dockerRun("stop", d.containerName)
}

// Destroy stops and removes the container. Snapshot images are intentionally
// preserved so that TryRestoreBaseImage can find them after the container is
// recreated. Images are only removed in destroyWithImages, called from
// DestroyAll at test teardown.
func (d *DockerVM) Destroy(_ context.Context) error {
	_ = dockerRun("stop", d.containerName)
	_ = dockerRun("rm", "-f", d.containerName)
	return nil
}

// destroyWithImages removes the container and all snapshot images committed for
// this profile. Called from DestroyAll during test cleanup.
func (d *DockerVM) destroyWithImages() {
	_ = dockerRun("stop", d.containerName)
	_ = dockerRun("rm", "-f", d.containerName)

	d.mu.Lock()
	snaps := append([]string(nil), d.snapshots...)
	d.snapshots = nil
	d.mu.Unlock()

	for _, tag := range snaps {
		_ = dockerRun("rmi", "-f", tag)
	}
}

// Run executes script inside the container as the test user.
// The script is base64-encoded to avoid shell quoting issues, mirroring the
// approach used by ColimaVM.Run.
func (d *DockerVM) Run(_ context.Context, script string, env map[string]string) error {
	d.mu.Lock()
	d.runCount++
	d.mu.Unlock()

	return dockerRun(
		"exec",
		"-u", testContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env),
	)
}

// RunOutput executes script inside the container as the test user and returns
// its combined stdout output. Use this when you need to assert on the content
// produced by a script, rather than just whether it succeeded.
func (d *DockerVM) RunOutput(_ context.Context, script string, env map[string]string) (string, error) {
	return dockerOutput(
		"exec",
		"-u", testContainerUser,
		d.containerName,
		"bash", "-lc", buildBashCmd(script, env),
	)
}

// buildBashCmd builds the bash -lc command string that executes the given
// script inside the container. The script is base64-encoded to avoid quoting
// issues, then decoded into a temporary file and executed as a login shell.
// Using a temp file (rather than piping directly to bash) prevents dpkg/apt
// postinst scripts from accidentally consuming stdin and eating the rest of
// the setup script, which would cause silent partial execution.
func buildBashCmd(script string, env map[string]string) string {
	full := script
	if len(env) > 0 {
		var sb strings.Builder
		for k, v := range env {
			fmt.Fprintf(&sb, "export %s=%s\n", k, vm.ShellEscape(v))
		}
		full = sb.String() + script
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(full))
	// Write decoded script to a temp file, execute as login shell, clean up.
	return "t=$(mktemp) && echo " + encoded + " | base64 -d > \"$t\" && bash -l \"$t\"; ec=$?; rm -f \"$t\"; exit $ec"
}

// SSH is a no-op for Docker-based test VMs. Interactive PTY sessions require
// a real terminal, which automated tests do not have. TestSSHAutoStart exercises
// the auto-start and bootstrap path inside LifecycleService.SSH; the shell
// session itself is out of scope for automated tests.
func (d *DockerVM) SSH(_ context.Context) error {
	return nil
}

// WaitReady polls until the container responds to a simple echo command.
func (d *DockerVM) WaitReady(_ context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := dockerRun("exec", "-u", testContainerUser, d.containerName, "echo", "ready"); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("container %s did not become ready within %s", d.containerName, timeout)
}

// CreateSnapshot commits the container's current filesystem as a Docker image.
func (d *DockerVM) CreateSnapshot(_ context.Context, name string) error {
	tag := d.snapshotTag(name)
	if err := dockerRun("commit", d.containerName, tag); err != nil {
		return fmt.Errorf("create snapshot %q: %w", name, err)
	}
	d.mu.Lock()
	d.snapshots = append(d.snapshots, tag)
	d.mu.Unlock()
	return nil
}

// RestoreSnapshot stops the container, removes it, and recreates it from the
// snapshot image. Returns (false, nil) when the snapshot does not exist.
func (d *DockerVM) RestoreSnapshot(_ context.Context, name string) (bool, error) {
	tag := d.snapshotTag(name)
	if _, err := dockerOutput("inspect", "--type", "image", tag); err != nil {
		return false, nil
	}

	_ = dockerRun("stop", d.containerName)
	_ = dockerRun("rm", "-f", d.containerName)

	if err := dockerRun("run", "-d", "--name", d.containerName, tag); err != nil {
		return false, fmt.Errorf("restore snapshot %q: %w", name, err)
	}
	return true, nil
}

// ListSnapshots returns the snapshot names recorded for this container.
func (d *DockerVM) ListSnapshots(_ context.Context) ([]vm.Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	snaps := make([]vm.Snapshot, 0, len(d.snapshots))
	for _, tag := range d.snapshots {
		// Extract the name portion after the last "-snap-<profile>-" prefix.
		parts := strings.SplitN(tag, "-snap-", 2)
		name := tag
		if len(parts) == 2 {
			name = strings.SplitN(parts[1], ":", 1)[0]
			// Remove the profile prefix: "<profile>-<name>:latest" → "<name>"
			profileDash := d.profile + "-"
			name = strings.TrimPrefix(name, profileDash)
		}
		snaps = append(snaps, vm.Snapshot{Name: name})
	}
	return snaps, nil
}

// RunCount returns the number of Run() calls since the last ResetRunCount.
func (d *DockerVM) RunCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.runCount
}

// ResetRunCount sets the run counter back to zero.
func (d *DockerVM) ResetRunCount() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runCount = 0
}

func (d *DockerVM) snapshotTag(name string) string {
	// Use a safe naming convention for Docker image tags.
	safe := strings.NewReplacer(" ", "-", "/", "-", ":", "-").Replace(name)
	return fmt.Sprintf("aivm-test-snap-%s-%s:latest", d.profile, safe)
}

// ── ContainerVMRegistry ────────────────────────────────────────────────────

// ContainerVMRegistry is a per-profile store of DockerVM instances.
type ContainerVMRegistry struct {
	mu  sync.Mutex
	vms map[string]*DockerVM
}

// NewContainerVMRegistry returns an empty registry.
func NewContainerVMRegistry() *ContainerVMRegistry {
	return &ContainerVMRegistry{vms: make(map[string]*DockerVM)}
}

// Register adds an existing DockerVM to the registry under its profile name.
func (r *ContainerVMRegistry) Register(d *DockerVM) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vms[d.profile] = d
}

// Get returns the DockerVM for the given profile, or nil if not found.
func (r *ContainerVMRegistry) Get(profile string) *DockerVM {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.vms[profile]
}

// GetOrCreate returns the DockerVM for the given profile, creating it if needed.
func (r *ContainerVMRegistry) GetOrCreate(profile, stateDir string) *DockerVM {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.vms[profile]; ok {
		return d
	}
	d := newDockerVM(profile, stateDir)
	r.vms[profile] = d
	return d
}

// DestroyAll removes all containers and snapshot images tracked in this registry.
// Called from t.Cleanup to ensure test isolation.
func (r *ContainerVMRegistry) DestroyAll() {
	r.mu.Lock()
	vms := make([]*DockerVM, 0, len(r.vms))
	for _, d := range r.vms {
		vms = append(vms, d)
	}
	r.mu.Unlock()

	for _, d := range vms {
		d.destroyWithImages()
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// dockerRun runs a docker command, discarding stdout. Stderr is returned as
// part of any error.
func dockerRun(args ...string) error {
	_, err := dockerOutput(args...)
	return err
}

// dockerOutput runs a docker command and returns combined stdout, or an error
// that includes stderr output for debugging.
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

// EnsureTestImage checks whether the test base image exists and builds it if not.
func EnsureTestImage(testDockerDir string) error {
	ensureTestImageOnce.Do(func() {
		out, _ := dockerOutput("images", "-q", testImageName)
		if strings.TrimSpace(out) != "" {
			return
		}

		cmd := exec.Command("docker", "build", "-t", testImageName, testDockerDir)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if err := cmd.Run(); err != nil {
			ensureTestImageErr = fmt.Errorf("build test image %s: %w\n%s", testImageName, err, buf.String())
		}
	})

	return ensureTestImageErr
}
