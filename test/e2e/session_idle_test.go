package e2e

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestSessionBlocksIdleMonitor verifies that an active session prevents the
// idle monitor from stopping the VM.
//
//  1. Start VM — idle monitor is started automatically by `aivm start`.
//  2. Launch aivm in the background (launch_command: "sleep 30") to hold a
//     real session lock file open.
//  3. Wait longer than the idle timeout — VM should NOT stop.
//  4. Cancel the background session (close the lock).
//  5. VM stops after the next idle poll cycle.
func TestSessionBlocksIdleMonitor(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithLaunchCommand("sleep 30"),
		framework.WithIdleTimeout(3*time.Second),
		framework.WithDeleteTimeout(5*time.Minute), // keep long so Phase 2 doesn't trigger
		framework.WithPollInterval(1*time.Second),
	)

	cancelSession, bgLaunch := actions.AsyncCLI()

	h.Scenario("active session blocks idle monitor from stopping the VM").
		Step("Start VM (idle monitor starts automatically)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session is registered", conditions.SessionCount(1), 15*time.Second).
		Assert("Session is registered", assertions.SessionCount(1)).
		Step("Wait longer than idle timeout (VM should NOT stop)", sleepStep(7*time.Second)).
		Assert("VM still running — session blocked idle stop",
			assertions.VMStatus(vm.StatusRunning)).
		Step("Cancel background session (idle timer can now elapse)", cancelSession).
		Wait("VM stopped after session ended", conditions.VMStatus(vm.StatusStopped), 30*time.Second).
		Assert("VM stopped now that session is gone", assertions.VMStatus(vm.StatusStopped)).
		Run()
}

// TestNoSessionsStopsVM verifies that with no active sessions, the idle monitor
// stops the VM after the idle timeout elapses.
//
//  1. Start VM — idle monitor is started automatically by `aivm start`.
//  2. No sessions — wait for idle timeout.
//  3. VM is stopped.
func TestNoSessionsStopsVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithIdleTimeout(3*time.Second),
		framework.WithDeleteTimeout(5*time.Minute),
		framework.WithPollInterval(1*time.Second),
	)

	h.Scenario("no sessions — idle monitor stops VM after timeout").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("No sessions at startup", assertions.SessionCount(0)).
		Wait("VM stopped after idle timeout", conditions.VMStatus(vm.StatusStopped), 30*time.Second).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Run()
}
