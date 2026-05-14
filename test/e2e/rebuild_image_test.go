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
// prompting.
//
// A real aivm session is held open via a background "sleep 30" launch command.
// The rebuild kills the session and completes.
func TestRebuildImageForceWithSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithLaunchCommand("sleep 30"))

	cancelSession, bgLaunch := actions.AsyncCLI()

	h.Scenario("force rebuild proceeds without prompts even when sessions are active").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session registered", conditions.SessionCount(1), 15*time.Second).
		Assert("One session is active before rebuild", assertions.SessionCount(1)).
		Step("Force rebuild — SIGTERM sent to sessions, rebuild proceeds", actions.CLI("rebuild-image", "--force")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Rebuild succeeded — VM is running", assertions.VMStatus(vm.StatusRunning)).
		Assert("New base image saved after rebuild", assertions.BaseImageExists()).
		Step("Cancel background session goroutine (already killed by rebuild)", cancelSession).
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
// A real session is held via a background "sleep 30" launch command.
// The rebuild prompt is answered "y" — sessions are killed and rebuild proceeds.
func TestRebuildImageInteractiveKillSessionsThenRebuild(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithLaunchCommand("sleep 30"),
		framework.WithInteractive("y"), // prompt: kill sessions and rebuild
	)

	cancelSession, bgLaunch := actions.AsyncCLI()

	h.Scenario("interactive rebuild with sessions — kill sessions and rebuild").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session registered", conditions.SessionCount(1), 15*time.Second).
		Assert("One session is active", assertions.SessionCount(1)).
		Step("Rebuild image (non-force) — asked to kill sessions, answer 'y'",
			actions.CLI("rebuild-image")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Step("Cancel background goroutine (session already killed by rebuild)", cancelSession).
		Run()
}
