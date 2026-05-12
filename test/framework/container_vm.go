package framework

import (
	"sync"

	"github.com/sisimomo/aivm/internal/vm"
)

const (
	// TestImageName is the Docker image used for all e2e test containers.
	// It is built automatically by BuildTestImage() at the start of each test run.
	TestImageName = "aivm-test-base:latest"
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

