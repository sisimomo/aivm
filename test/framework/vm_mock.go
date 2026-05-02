// Package framework provides the integration testing harness for AIVM.
package framework

import (
	"context"
	"sync"
	"time"

	"aivm/internal/vm"
)

// MockVM is an in-process fake VM that simulates the VM lifecycle in memory.
// Transitions are instantaneous and all scripts succeed trivially.
//
// MockVM satisfies the vm.VM interface and can be used anywhere a real
// ColimaVM is expected.
type MockVM struct {
	mu        sync.Mutex
	profile   string
	stateDir  string
	status    vm.Status
	snapshots map[string]vm.Status
	runCount  int // number of vm.Run() calls since last ResetRunCount
}

func newMockVM(profile, stateDir string) *MockVM {
	return &MockVM{
		profile:   profile,
		stateDir:  stateDir,
		status:    vm.StatusNotFound,
		snapshots: map[string]vm.Status{},
	}
}

// ensure MockVM implements vm.VM at compile time
var _ vm.VM = (*MockVM)(nil)

func (m *MockVM) Profile() string { return m.profile }

func (m *MockVM) Status(_ context.Context) (vm.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status, nil
}

func (m *MockVM) Start(_ context.Context, _ vm.StartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = vm.StatusRunning
	return nil
}

func (m *MockVM) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == vm.StatusRunning {
		m.status = vm.StatusStopped
	}
	return nil
}

func (m *MockVM) Destroy(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = vm.StatusNotFound
	m.snapshots = map[string]vm.Status{}
	return nil
}

// Run returns nil immediately — all scripts are treated as successful.
// MockVM tests the AIVM lifecycle logic, not the scripts themselves
// (those require a real VM). Each call increments RunCount.
func (m *MockVM) Run(_ context.Context, _ string, _ map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCount++
	return nil
}

// RunCount returns the number of vm.Run() calls since the last ResetRunCount.
func (m *MockVM) RunCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runCount
}

// ResetRunCount sets the run counter back to zero.
func (m *MockVM) ResetRunCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCount = 0
}

func (m *MockVM) SSH(_ context.Context) error { return nil }

func (m *MockVM) WaitReady(_ context.Context, _ time.Duration) error { return nil }

func (m *MockVM) CreateSnapshot(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[name] = m.status
	return nil
}

func (m *MockVM) RestoreSnapshot(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.snapshots[name]
	if !ok {
		return false, nil
	}
	m.status = s
	return true, nil
}

func (m *MockVM) ListSnapshots(_ context.Context) ([]vm.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snaps := make([]vm.Snapshot, 0, len(m.snapshots))
	for name := range m.snapshots {
		snaps = append(snaps, vm.Snapshot{Name: name})
	}
	return snaps, nil
}

// MockVMRegistry is a per-profile store of MockVM instances.
// The factory it produces creates (or retrieves) a named MockVM for each
// profile, so tests can inspect the state of secondary VMs (e.g. the legacy
// VM destroyed during a soft rebuild or the legacy monitor cycle).
type MockVMRegistry struct {
	mu  sync.Mutex
	vms map[string]*MockVM
}

// NewMockVMRegistry returns an empty registry.
func NewMockVMRegistry() *MockVMRegistry {
	return &MockVMRegistry{vms: make(map[string]*MockVM)}
}

// Register adds an existing MockVM to the registry under its profile name.
func (r *MockVMRegistry) Register(m *MockVM) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vms[m.profile] = m
}

// Get returns the MockVM for the given profile, or nil if not found.
func (r *MockVMRegistry) Get(profile string) *MockVM {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.vms[profile]
}

// Factory returns a vm.VMFactory that creates or retrieves a MockVM per profile.
// The first call for a given profile creates a new MockVM; subsequent calls
// return the same instance.
func (r *MockVMRegistry) Factory() vm.VMFactory {
	return func(profile, stateDir string) vm.VM {
		r.mu.Lock()
		defer r.mu.Unlock()
		if m, ok := r.vms[profile]; ok {
			return m
		}
		m := newMockVM(profile, stateDir)
		r.vms[profile] = m
		return m
	}
}

