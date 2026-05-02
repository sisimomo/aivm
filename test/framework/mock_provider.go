package framework

import (
	"context"
	"sync/atomic"

	"aivm/internal/agent"
)

// MockProvider wraps a real agent.Provider and replaces Launch with a no-op
// that records call counts. This lets tests verify that DoLaunch reached the
// agent dispatch step without SSH-ing into a real VM.
//
// All other methods (Name, Description, RequiredPlugins) delegate to the real
// provider so that bootstrap plugin wiring continues to work correctly.
type MockProvider struct {
	real        agent.Provider
	launchCalls int64 // accessed atomically
}

func newMockProvider(real agent.Provider) *MockProvider {
	return &MockProvider{real: real}
}

func (m *MockProvider) Name() string        { return m.real.Name() }
func (m *MockProvider) Description() string { return m.real.Description() }

// RequiredPlugins delegates to the real provider so that bootstrap plugin
// wiring and agent mismatch detection work correctly. Plugin scripts are
// replaced with trivial test stubs in the harness (see harness.go), so
// installing these plugins is fast even though the names match real ones.
func (m *MockProvider) RequiredPlugins() []string { return m.real.RequiredPlugins() }

// Launch records the call and returns immediately with exit code 0.
// No SSH or subprocess is started.
func (m *MockProvider) Launch(_ context.Context, _ agent.LaunchEnv) (*agent.Response, error) {
	atomic.AddInt64(&m.launchCalls, 1)
	return &agent.Response{ExitCode: 0}, nil
}

// LaunchCallCount returns the number of times Launch was called.
func (m *MockProvider) LaunchCallCount() int {
	return int(atomic.LoadInt64(&m.launchCalls))
}
