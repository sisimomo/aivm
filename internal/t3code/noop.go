package t3code

import (
	"context"
	"sync/atomic"
)

// NoopManager is a no-op implementation of Manager used in e2e tests.
// It records call counts instead of running real SSH processes.
type NoopManager struct {
	launchCount atomic.Int32
	running     atomic.Bool
}

func (n *NoopManager) Launch(_ context.Context, _ int) error {
	n.launchCount.Add(1)
	n.running.Store(true)
	return nil
}

func (n *NoopManager) Stop() error {
	n.running.Store(false)
	return nil
}

func (n *NoopManager) IsRunning() bool {
	return n.running.Load()
}

// LaunchCallCount returns the number of times Launch has been called.
func (n *NoopManager) LaunchCallCount() int {
	return int(n.launchCount.Load())
}
