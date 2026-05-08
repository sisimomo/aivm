package framework

import (
	"context"
	"sync"

	"github.com/sisimomo/aivm/internal/vm"
)

// RunTrackingVM wraps a vm.VM and counts every Run and RunOutput call.
// It implements RunCounter so assertions can verify how many scripts the
// lifecycle service executed without coupling to a concrete backend.
type RunTrackingVM struct {
	vm.VM
	mu    sync.Mutex
	count int
}

// NewRunTrackingVM returns a RunTrackingVM wrapping v with its counter at zero.
func NewRunTrackingVM(v vm.VM) *RunTrackingVM {
	return &RunTrackingVM{VM: v}
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
