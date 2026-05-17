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

// TestRecreateRebuildForceNoSessions verifies that `aivm recreate --rebuild` with
// no active sessions destroys the current VM, runs a fresh bootstrap, and saves
// a new base image — all without any interactive prompts.
//
//  1. Start VM → base image v1 saved.
//  2. Wait 1s so the new image gets a different Unix timestamp.
//  3. RecreateVM(rebuild=true) — no prompts, immediate rebuild.
//  4. VM is running again with a new base image (v2 ≠ v1).
func TestRecreateRebuildForceNoSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	var v1ID string

	h.Scenario("recreate --rebuild with no sessions — no prompts, new base image").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Force rebuild with no active sessions (no prompts)", actions.CLI("recreate", "--rebuild")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Assert("Base image ID changed after rebuild",
			assertions.BaseImageIsNot(&v1ID)).
		Assert("VM image ref is current (v2)", assertions.VMImageRefCurrent()).
		Run()
}

// TestRecreateRebuildForceWithSessions verifies that `aivm recreate --rebuild`
// sends SIGTERM to all active sessions and proceeds with the rebuild without
// prompting.
//
// A real aivm session is held open via a background "sleep 30" launch command.
// The rebuild kills the session and completes.
func TestRecreateRebuildForceWithSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithLaunchCommand("sleep 30"))

	cancelSession, bgLaunch := actions.AsyncCLI()

	h.Scenario("recreate --rebuild proceeds without prompts even when sessions are active").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session registered", conditions.SessionCount(1), 15*time.Second).
		Assert("One session is active before rebuild", assertions.SessionCount(1)).
		Step("Force rebuild — SIGTERM sent to sessions, rebuild proceeds", actions.CLI("recreate", "--rebuild")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Rebuild succeeded — VM is running", assertions.VMStatus(vm.StatusRunning)).
		Assert("New base image saved after rebuild", assertions.BaseImageExists()).
		Step("Cancel background session goroutine (already killed by rebuild)", cancelSession).
		Run()
}

// TestRecreateRebuildInteractiveConfirm verifies that `aivm recreate` (no --rebuild)
// with no base image prompts the user and proceeds with a full rebuild when answered "y".
//
//  1. Start VM (base image saved).
//  2. RecreateVM(rebuild=false) in interactive mode — no base image prompt answered "y".
//  3. VM is rebuilt; new base image saved.
func TestRecreateRebuildInteractiveConfirm(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("y"), // "Run a full rebuild now? [y/N]"
	)

	h.Scenario("interactive recreate without base image — user confirms with 'y'").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("No active sessions", assertions.SessionCount(0)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Recreate (non-rebuild) — prompt shown, answered 'y'",
			actions.CLI("recreate")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Assert("VM image ref is current", assertions.VMImageRefCurrent()).
		Run()
}

// TestRecreateRebuildInteractiveCancel verifies that `aivm recreate` does nothing
// when the user answers "n" at the no-base-image prompt.
//
//  1. Start VM → base image v1 saved.
//  2. RecreateVM(rebuild=false) in interactive mode, answer "n".
//  3. VM is still running; base image is unchanged (still v1).
func TestRecreateRebuildInteractiveCancel(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("n"), // "Run a full rebuild now? [y/N]"
	)

	var v1ID string

	h.Scenario("interactive recreate — user cancels with 'n'").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Recreate (non-rebuild) — prompt shown, answered 'n'",
			actions.CLI("recreate")).
		Assert("VM still running (recreate was cancelled)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Base image unchanged (still v1)", assertions.BaseImageIs(&v1ID)).
		Run()
}

// TestRecreateRebuildInteractiveKillSessionsThenRebuild verifies the flow where
// the user has active sessions, runs `aivm recreate --rebuild`, sessions are
// killed, and the rebuild proceeds.
//
// A real session is held via a background "sleep 30" launch command.
func TestRecreateRebuildInteractiveKillSessionsThenRebuild(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithLaunchCommand("sleep 30"),
		framework.WithInteractive("y"), // prompt: kill sessions and rebuild
	)

	cancelSession, bgLaunch := actions.AsyncCLI()

	h.Scenario("recreate --rebuild with sessions — kill sessions and rebuild").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session registered", conditions.SessionCount(1), 15*time.Second).
		Assert("One session is active", assertions.SessionCount(1)).
		Step("Rebuild image (--rebuild) — sessions killed, rebuild proceeds",
			actions.CLI("recreate", "--rebuild")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Step("Cancel background goroutine (session already killed by rebuild)", cancelSession).
		Run()
}

// TestRebuildImageShadowVM verifies that `aivm rebuild-image` rebuilds the base
// image using a shadow VM without stopping or destroying the running primary VM.
//
//  1. Start VM → base image v1 saved.
//  2. Launch a background session (holds session lock).
//  3. Run `aivm rebuild-image` — shadow VM bootstraps, new base image v2 saved.
//  4. Primary VM is still running; active session is still present; base image changed.
func TestRebuildImageShadowVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithLaunchCommand("sleep 300"),
		framework.WithIdleTimeout(10*time.Minute),
		framework.WithDeleteTimeout(10*time.Minute),
	)

	cancelSession, bgLaunch := actions.AsyncCLI()
	var v1ID string

	h.Scenario("rebuild-image uses shadow VM — running VM and sessions untouched").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Launch aivm in background (holds session lock)", bgLaunch).
		Wait("Session registered", conditions.SessionCount(1), 15*time.Second).
		Assert("One session is active before rebuild", assertions.SessionCount(1)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Run: aivm rebuild-image (shadow VM)", actions.CLI("rebuild-image")).
		Assert("Primary VM still running after rebuild", assertions.VMStatus(vm.StatusRunning)).
		Assert("Session still active after rebuild", assertions.SessionCount(1)).
		Assert("Base image ID changed after rebuild", assertions.BaseImageIsNot(&v1ID)).
		Step("Cancel background session goroutine", cancelSession).
		Run()
}

// TestRebuildImageThenRecreate verifies the full workflow: rebuild-image to refresh
// the base image, then recreate to apply it to the running VM.
//
//  1. Start VM → base image v1.
//  2. rebuild-image → base image v2 (shadow VM, primary untouched).
//  3. recreate → VM restarted from v2 snapshot.
//  4. VM image ref now matches v2.
func TestRebuildImageThenRecreate(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithIdleTimeout(10*time.Minute),
		framework.WithDeleteTimeout(10*time.Minute),
	)

	var v1ID string

	h.Scenario("rebuild-image then recreate — full image refresh workflow").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Rebuild base image via shadow VM", actions.CLI("rebuild-image")).
		Assert("Primary VM still running after rebuild", assertions.VMStatus(vm.StatusRunning)).
		Assert("Base image ID changed after rebuild", assertions.BaseImageIsNot(&v1ID)).
		Step("Recreate VM from new base image", actions.CLI("recreate")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM image ref is current (v2)", assertions.VMImageRefCurrent()).
		Run()
}
//
//  1. Start VM → base image v1 saved.
//  2. Recreate (no --rebuild) → VM stopped, restored from snapshot, restarted.
//  3. VM is running; base image is still v1 (snapshot was restored, not rebuilt).
func TestRecreateFromSnapshot(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	var v1ID string

	h.Scenario("fast recreate from snapshot — no bootstrap, base image unchanged").
		Step("Start VM (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Recreate from snapshot (fast path)", actions.CLI("recreate")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM is running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Base image unchanged (still v1)", assertions.BaseImageIs(&v1ID)).
		Run()
}
