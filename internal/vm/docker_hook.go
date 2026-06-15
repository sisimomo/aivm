package vm

import (
	"context"
	"sync"
)

// DockerExecHook substitutes docker CLI calls in tests.
type DockerExecHook func(ctx context.Context, args ...string) (stdout string, err error)

var (
	dockerExecHookMu sync.Mutex
	dockerExecHook   DockerExecHook
)

func currentDockerExecHook() DockerExecHook {
	dockerExecHookMu.Lock()
	defer dockerExecHookMu.Unlock()
	return dockerExecHook
}

// SetDockerExecHookForTest installs a test double for docker CLI calls and
// returns a function that restores the default behavior.
func SetDockerExecHookForTest(hook DockerExecHook) func() {
	dockerExecHookMu.Lock()
	prev := dockerExecHook
	dockerExecHook = hook
	dockerExecHookMu.Unlock()
	return func() {
		dockerExecHookMu.Lock()
		dockerExecHook = prev
		dockerExecHookMu.Unlock()
	}
}
