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

// TestRebuildImageForceNoSessions verifies that DoRebuildImage(force=true) with
// no active sessions destroys the current VM, runs a fresh bootstrap, and saves
// a new base image — all without any interactive prompts.
//
//  1. Start VM → base image v1 saved.
//  2. Wait 1s so the new image gets a different Unix timestamp.
//  3. RebuildImage(force=true) — no prompts, immediate rebuild.
//  4. VM is running again with a new base image (v2 ≠ v1).
func TestRebuildImageForceNoSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	var v1ID string

	h.Scenario("force rebuild with no sessions — no prompts, new base image").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Force rebuild with no active sessions (no prompts)", actions.CLI("rebuild-image", "--force")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Assert("Base image ID changed after rebuild",
			assertions.BaseImageIsNot(&v1ID)).
		Assert("VM image ref is current (v2)", assertions.VMImageRefCurrent()).
		Run()
}

// TestRebuildImageForceWithSessions verifies that DoRebuildImage(force=true)
// sends SIGTERM to all active sessions and proceeds with the rebuild without
// prompting. The rebuild succeeds regardless of active sessions.
//
// Note: in production the session lock files are removed when those processes
// exit. In tests the "fake session" PID is the test process itself, which
// survives SIGTERM; we verify the rebuild completed rather than session count.
func TestRebuildImageForceWithSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("force rebuild proceeds without prompts even when sessions are active").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create a fake active session", actions.CreateFakeSession()).
		Assert("One session is active before rebuild", assertions.SessionCount(1)).
		Step("Force rebuild — SIGTERM sent to sessions, rebuild proceeds", actions.CLI("rebuild-image", "--force")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Rebuild succeeded — VM is running", assertions.VMStatus(vm.StatusRunning)).
		Assert("New base image saved after rebuild", assertions.BaseImageExists()).
		Step("Clean up fake session lock file", actions.RemoveFakeSessions()).
		Run()
}

// TestRebuildImageInteractiveConfirm verifies that DoRebuildImage(force=false)
// with no sessions prompts the user and proceeds when the answer is "y".
//
//  1. Start VM.
//  2. RebuildImage(force=false) in interactive mode, answer "y".
//  3. VM is rebuilt; new base image saved.
func TestRebuildImageInteractiveConfirm(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("y"), // "Proceed with base image rebuild? [y/N]"
	)

	h.Scenario("interactive rebuild — user confirms with 'y'").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("No active sessions", assertions.SessionCount(0)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Rebuild image (non-force) — prompt shown, answered 'y'",
			actions.CLI("rebuild-image")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Assert("VM image ref is current", assertions.VMImageRefCurrent()).
		Run()
}

// TestRebuildImageInteractiveCancel verifies that DoRebuildImage(force=false)
// does nothing when the user answers "n" at the confirmation prompt.
//
//  1. Start VM → base image v1 saved.
//  2. RebuildImage(force=false) in interactive mode, answer "n".
//  3. VM is still running; base image is unchanged (still v1).
func TestRebuildImageInteractiveCancel(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("n"), // "Proceed with base image rebuild? [y/N]"
	)

	var v1ID string

	h.Scenario("interactive rebuild — user cancels with 'n'").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Rebuild image (non-force) — prompt shown, answered 'n'",
			actions.CLI("rebuild-image")).
		Assert("VM still running (rebuild was cancelled)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Base image unchanged (still v1)", assertions.BaseImageIs(&v1ID)).
		Run()
}

// TestRebuildImageInteractiveKillSessionsThenRebuild verifies the flow where
// the user has active sessions and chooses to kill them and rebuild now.
//
//  1. Start VM.
//  2. Create a fake session.
//  3. RebuildImage(force=false): "Kill all sessions? [y/N]" → "y" kills sessions and rebuilds.
func TestRebuildImageInteractiveKillSessionsThenRebuild(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("y", "y"), // prompt 1: kill sessions, prompt 2: proceed with rebuild
	)

	h.Scenario("interactive rebuild with sessions — kill sessions and rebuild").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create a fake active session", actions.CreateFakeSession()).
		Assert("One session is active", assertions.SessionCount(1)).
		Step("Rebuild image (non-force) — asked to kill sessions, answer 'y'",
			actions.CLI("rebuild-image")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		// KillAll sends SIGTERM but does not remove lock files — processes clean
		// up their own lock files on exit. We remove the fake lock file here to
		// simulate the child exiting after receiving SIGTERM.
		Step("Simulate session exiting (remove lock file)", actions.RemoveFakeSessions()).
		Assert("No sessions remain after rebuild", assertions.SessionCount(0)).
		Run()
}

// TestRebuildImageSoftRebuild verifies the soft-rebuild flow: when the user has
// active sessions and declines to kill them, then agrees to a soft rebuild, a
// second VM is bootstrapped, a transition state is written, and the legacy monitor
// is started. The legacy VM remains until the old sessions drain.
//
//  1. Start VM.
//  2. Create a fake active session.
//  3. RebuildImage(force=false):
//     Prompt 1 "Kill all sessions? [y/N]" → "n"
//     Prompt 2 "Proceed with soft rebuild? [y/N]" → "y"
//  4. Transition state written; new VM profile created.
//  5. Legacy VM (primary profile) is still running.
func TestRebuildImageSoftRebuild(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("n", "y"), // prompt 1: n, prompt 2: y
	)

	h.Scenario("soft rebuild — keep sessions on legacy VM, bootstrap new VM in parallel").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Create a fake active session", actions.CreateFakeSession()).
		Assert("One active session", assertions.SessionCount(1)).
		Step("Rebuild image — keep sessions, approve soft rebuild",
			actions.CLI("rebuild-image")).
		Assert("Transition state written (new VM ready)", assertions.TransitionStateExists()).
		Assert("Legacy VM still running for active sessions",
			assertions.VMStatus(vm.StatusRunning)).
		Step("Remove fake session (simulates session ending)", actions.RemoveFakeSessions()).
		Run()
}
