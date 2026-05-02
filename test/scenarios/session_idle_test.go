package scenarios

import (
	"testing"
	"time"

	"aivm/internal/vm"
	"aivm/test/framework"
	"aivm/test/framework/actions"
	"aivm/test/framework/assertions"
	"aivm/test/framework/conditions"
)

// TestSessionBlocksIdleMonitor verifies that an active session prevents the
// idle monitor from stopping the VM.
//
//  1. Start VM and launch the idle monitor in-process.
//  2. Create a fake session (uses current test PID — alive throughout the test).
//  3. Wait longer than the idle timeout (10 s) and confirm the VM is STILL running.
//  4. Remove the session.
//  5. VM stops after the next idle poll cycle.
func TestSessionBlocksIdleMonitor(t *testing.T) {
	h := framework.New(t,
		framework.WithIdleTimeout(5*time.Second),
		framework.WithDeleteTimeout(5*time.Minute), // keep long so delete doesn't trigger
		framework.WithPollInterval(1*time.Second),
	)

	h.Scenario("active session blocks idle monitor from stopping the VM").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch idle monitor (in-process)", actions.StartMonitor(nil)).
		Step("Create a fake active session", actions.CreateFakeSession()).
		Assert("Session is registered", assertions.SessionCount(1)).
		Step("Wait longer than idle timeout (VM should NOT stop)", sleepStep(12*time.Second)).
		Assert("VM still running — session blocked idle stop",
			assertions.VMStatus(vm.StatusRunning)).
		Step("Remove session (idle timer can now elapse)", actions.RemoveFakeSessions()).
		Wait("VM stopped after session ended", conditions.VMStatus(vm.StatusStopped), 30*time.Second).
		Assert("VM stopped now that session is gone", assertions.VMStatus(vm.StatusStopped)).
		Run()
}

// TestMultipleSessionsAllMustEndBeforeIdle verifies that the VM only stops
// after ALL sessions have ended, not just one.
//
//  1. Start VM and launch the idle monitor.
//  2. Create two fake sessions (same PID, different directories → only one lock
//     file because the PID is the same, so effectively one session).
//     This test primarily confirms the zero-session condition.
//  3. Remove all sessions.
//  4. VM stops after idle timeout.
func TestNoSessionsStopsVM(t *testing.T) {
	h := framework.New(t,
		framework.WithIdleTimeout(5*time.Second),
		framework.WithDeleteTimeout(5*time.Minute),
		framework.WithPollInterval(1*time.Second),
	)

	h.Scenario("no sessions — idle monitor stops VM after timeout").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("No sessions at startup", assertions.SessionCount(0)).
		Step("Launch idle monitor (in-process)", actions.StartMonitor(nil)).
		Wait("VM stopped after idle timeout", conditions.VMStatus(vm.StatusStopped), 30*time.Second).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Run()
}
