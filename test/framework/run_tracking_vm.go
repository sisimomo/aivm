package framework

import (
	"context"
	"sync"

	"github.com/sisimomo/aivm/internal/vm"
)

// RunTrackingVM wraps a vm.VM and counts every Run and RunOutput call.
// It implements RunCounter so assertions can verify how many scripts the
// lifecycle service executed without coupling to a concrete backend.
//
// When wrapping a DockerVM with T3Code enabled and port auto-assignment (port 0),
// it also intercepts Start to retrieve the actual assigned port and update the
// harness's t3codePort field, eliminating the TOCTOU race from findFreePort.
type RunTrackingVM struct {
	vm.VM
	mu              sync.Mutex
	count           int
	harness         *Harness
	t3CodeContainer int // container port for T3 Code (3773)
}

// NewRunTrackingVM returns a RunTrackingVM wrapping v with its counter at zero.
func NewRunTrackingVM(v vm.VM) *RunTrackingVM {
	return &RunTrackingVM{VM: v}
}

// setHarness is called by the harness after creation to enable port sync.
func (r *RunTrackingVM) setHarness(h *Harness, t3CodeContainerPort int) {
	r.harness = h
	r.t3CodeContainer = t3CodeContainerPort
}

func (r *RunTrackingVM) Start(ctx context.Context, opts vm.StartOptions) error {
	if err := r.VM.Start(ctx, opts); err != nil {
		return err
	}

	// If T3Code port auto-assignment was used (port 0), retrieve the actual assigned
	// host port from Docker and update the harness so tests can access it.
	if r.harness != nil && r.harness.tc.T3CodeEnabled && r.harness.tc.T3CodePort == 0 && r.t3CodeContainer > 0 {
		if dockerVM, ok := r.VM.(*vm.DockerVM); ok {
			if assignedPort, err := dockerVM.GetPublishedPort(r.t3CodeContainer); err == nil && assignedPort > 0 {
				r.harness.t3codePort = assignedPort
			}
		}
	}
	return nil
}

func (r *RunTrackingVM) Run(ctx context.Context, script string, env map[string]string) error {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	return r.VM.Run(ctx, script, env)
}

func (r *RunTrackingVM) RunOutput(ctx context.Context, script string, env map[string]string) (string, error) {
	r.mu.Lock()
	r.count++
	r.mu.Unlock()
	return r.VM.RunOutput(ctx, script, env)
}

// RunCount returns the number of Run/RunOutput calls since the last ResetRunCount.
func (r *RunTrackingVM) RunCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// ResetRunCount resets the counter to zero.
func (r *RunTrackingVM) ResetRunCount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count = 0
}
