package framework

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/sisimomo/aivm/internal/vm"
)

const (
	// TestImageName is the Docker image used for all integration test containers.
	// Build it with: docker build -t aivm-test-base:latest ./test/docker/
	TestImageName = "aivm-test-base:latest"

	// testImageName is a package-level alias for TestImageName for internal use.
	testImageName = TestImageName
)

var (
	ensureTestImageOnce sync.Once
	ensureTestImageErr  error
)

// ── ContainerVMRegistry ────────────────────────────────────────────────────

// ContainerVMRegistry is a per-profile store of DockerVM instances.
// It tracks every container created during a test so that DestroyAll can
// clean them up (including snapshot images) at teardown.
type ContainerVMRegistry struct {
	mu  sync.Mutex
	vms map[string]*vm.DockerVM
}

// NewContainerVMRegistry returns an empty registry.
func NewContainerVMRegistry() *ContainerVMRegistry {
	return &ContainerVMRegistry{vms: make(map[string]*vm.DockerVM)}
}

// Register adds an existing DockerVM to the registry under its profile name.
func (r *ContainerVMRegistry) Register(d *vm.DockerVM) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vms[d.Profile()] = d
}

// Get returns the DockerVM for the given profile, or nil if not found.
func (r *ContainerVMRegistry) Get(profile string) *vm.DockerVM {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.vms[profile]
}

// GetOrCreate returns the DockerVM for the given profile, creating it if
// needed. stateDir and image are only used on first creation.
func (r *ContainerVMRegistry) GetOrCreate(profile, stateDir, image string) *vm.DockerVM {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.vms[profile]; ok {
		return d
	}
	d := vm.NewDocker(profile, stateDir, image)
	r.vms[profile] = d
	return d
}

// DestroyAll removes all containers and snapshot images tracked in this registry.
// Called from t.Cleanup to ensure test isolation.
func (r *ContainerVMRegistry) DestroyAll() {
	r.mu.Lock()
	vms := make([]*vm.DockerVM, 0, len(r.vms))
	for _, d := range r.vms {
		vms = append(vms, d)
	}
	r.mu.Unlock()

	for _, d := range vms {
		d.DestroyWithImages()
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// EnsureTestImage checks whether the test base image exists and builds it if not.
func EnsureTestImage(testDockerDir string) error {
	ensureTestImageOnce.Do(func() {
		out, _ := testDockerOutput("images", "-q", testImageName)
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

// testDockerOutput runs a docker command and returns combined stdout, or an
// error that includes stderr for debugging. Only used by EnsureTestImage;
// the production DockerVM uses its own unexported dockerOutput helper.
func testDockerOutput(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

