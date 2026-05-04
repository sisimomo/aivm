package scenarios

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestIdleStopResume verifies Phase 1 of the idle lifecycle:
//
//  1. Start the VM and launch the idle monitor in-process.
//  2. With no active sessions, the monitor stops the VM after the idle timeout.
//  3. Calling Start again resumes the stopped VM (disk preserved — no bootstrap).
//
// Idle timeout and poll interval are set very short (10s / 1s) so the test
// completes in under a minute.
func TestIdleStopResume(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithCPUs(2),
		framework.WithMemoryGiB(4),
		framework.WithIdleTimeout(3*time.Second),
		framework.WithDeleteTimeout(5*time.Minute), // keep long so Phase 2 doesn't trigger
		framework.WithPollInterval(1*time.Second),
	)

	h.Scenario("idle → stop → resume").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM is running", assertions.VMStatus(vm.StatusRunning)).
		Step("Start idle monitor (in-process)", actions.StartMonitor(nil)).
		// Monitor polls every 1s; after 10s idle it stops the VM.
		Wait("VM auto-stopped by idle monitor", conditions.VMStatus(vm.StatusStopped), 60*time.Second).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Step("Resume VM via Start", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("VM is running after resume", assertions.VMStatus(vm.StatusRunning)).
		Run()
}
