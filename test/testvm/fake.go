package testvm

import (
	"context"
	"sync"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

// Call records one FakeVM method invocation for test assertions.
type Call struct {
	Method string
	Detail string
}

// Faults inject errors on specific methods. Zero values mean success.
type Faults struct {
	StartErr                error
	StopErr                 error
	DestroyErr              error
	SaveBaseImageErr        error
	RestoreFromBaseImageErr error
	DeleteBaseImageErr      error
	WaitReadyErr            error
}

// FakeVM is a stateful VM + base-image simulator for lifecycle integration tests.
type FakeVM struct {
	mu              sync.Mutex
	status          vm.Status
	baseImageExists bool
	calls           []Call
	faults          Faults
}

func New() *FakeVM {
	return &FakeVM{status: vm.StatusNotFound}
}

func (f *FakeVM) Profile() string { return "test" }

func (f *FakeVM) NeedsPortBindingAtBoot() bool { return true }

func (f *FakeVM) Status(_ context.Context) (vm.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *FakeVM) Start(_ context.Context, _ vm.StartOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("Start", "")
	if f.faults.StartErr != nil {
		return f.faults.StartErr
	}
	if f.status == vm.StatusNotFound || f.status == vm.StatusStopped {
		f.status = vm.StatusRunning
	}
	return nil
}

func (f *FakeVM) Stop(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("Stop", "")
	if f.faults.StopErr != nil {
		return f.faults.StopErr
	}
	if f.status == vm.StatusRunning {
		f.status = vm.StatusStopped
	}
	return nil
}

func (f *FakeVM) Destroy(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("Destroy", "")
	if f.faults.DestroyErr != nil {
		return f.faults.DestroyErr
	}
	f.status = vm.StatusNotFound
	return nil
}

func (f *FakeVM) Run(
	_ context.Context, script string, _ map[string]string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("Run", script)
	return nil
}

func (f *FakeVM) RunOutput(
	_ context.Context, script string, _ map[string]string,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("RunOutput", script)
	return "", nil
}

func (f *FakeVM) RunInteractive(
	_ context.Context, script string, _ map[string]string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("RunInteractive", script)
	return nil
}

func (f *FakeVM) RunStream(
	_ context.Context, script string, _ map[string]string,
) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("RunStream", script)
	return 0, nil
}

func (f *FakeVM) SSH(_ context.Context, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("SSH", "")
	return nil
}

func (f *FakeVM) CopyTo(_ context.Context, _, _ string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("CopyTo", "")
	return nil
}

func (f *FakeVM) CopyFrom(_ context.Context, _, _ string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("CopyFrom", "")
	return nil
}

func (f *FakeVM) WaitReady(_ context.Context, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("WaitReady", "")
	return f.faults.WaitReadyErr
}

func (f *FakeVM) GetPublishedPort(port int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("GetPublishedPort", "")
	return port, nil
}

func (f *FakeVM) SaveBaseImage(_ context.Context, _ vm.StartOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("SaveBaseImage", "")
	if f.faults.SaveBaseImageErr != nil {
		return f.faults.SaveBaseImageErr
	}
	f.baseImageExists = true
	return nil
}

func (f *FakeVM) RestoreFromBaseImage(
	_ context.Context, _ vm.StartOptions,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("RestoreFromBaseImage", "")
	if f.faults.RestoreFromBaseImageErr != nil {
		return f.faults.RestoreFromBaseImageErr
	}
	if !f.baseImageExists {
		return context.Canceled
	}
	f.status = vm.StatusRunning
	return nil
}

func (f *FakeVM) DeleteBaseImage(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendCall("DeleteBaseImage", "")
	if f.faults.DeleteBaseImageErr != nil {
		return f.faults.DeleteBaseImageErr
	}
	f.baseImageExists = false
	return nil
}

func (f *FakeVM) HasBaseImage(_ context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.baseImageExists
}

// --- test helpers ---

func (f *FakeVM) CallLog() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *FakeVM) HasCall(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Method == method {
			return true
		}
	}
	return false
}

func (f *FakeVM) CallCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (f *FakeVM) SetStatus(s vm.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *FakeVM) BaseImageExists() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.baseImageExists
}

func (f *FakeVM) SetBaseImageExists(exists bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.baseImageExists = exists
}

func (f *FakeVM) SetFaults(faults Faults) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = faults
}

func (f *FakeVM) ResetCallLog() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func (f *FakeVM) appendCall(method, detail string) {
	f.calls = append(f.calls, Call{Method: method, Detail: detail})
}
