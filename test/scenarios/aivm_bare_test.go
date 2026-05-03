package scenarios

// Tests for the bare `aivm` command — the most frequently used invocation.
//
// Running `aivm` with no arguments calls DoStart (ensure VM is running with
// bootstrap complete) followed by DoLaunch (start an AI agent session in the
// current working directory). This file verifies every meaningful code path
// through that two-step flow.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aivm/internal/vm"
	"aivm/test/framework"
	"aivm/test/framework/actions"
	"aivm/test/framework/assertions"
	"aivm/test/framework/conditions"
)

// TestBareCommandFirstBoot covers the most common first-time flow:
//
//  1. User runs `aivm` from a directory under DevRoot.
//  2. No VM exists yet — DoStart creates it, runs bootstrap, saves a base image.
//  3. DoLaunch verifies the CWD, checks VM is running, and starts the agent.
//
// After this test the agent should have been launched exactly once and the VM
// should be running (DoLaunch does not stop the VM).
func TestBareCommandFirstBoot(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) — first boot: DoStart + DoLaunch").
		Step("Run: aivm (bare)", actions.CLI()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap completed during DoStart", assertions.BootstrapComplete()).
		Assert("Base image saved during DoStart", assertions.BaseImageExists()).
		Assert("Agent was launched by DoLaunch", assertions.AgentLaunched()).
		Assert("Agent launched exactly once", assertions.AgentLaunchCount(1)).
		Assert("User saw bootstrap complete", assertions.OutputContains("Bootstrap complete!")).
		Assert("User saw agent launch step", assertions.OutputContains("Launching Claude Code (Anthropic) in VM")).
		Run()
}

// TestBareCommandResume verifies the second-most-common flow:
//
//  1. VM is already running from a previous session (simulate with `aivm start`).
//  2. User runs `aivm` — DoStart detects the VM is already up and skips bootstrap.
//  3. DoLaunch starts the agent directly.
func TestBareCommandResume(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) — VM already running: skip bootstrap, launch agent directly").
		Step("Pre-start VM so it is already running", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Reset run counter (nothing should re-run)", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm (bare) — VM already up", actions.CLI()).
		Assert("VM is still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("No bootstrap scripts ran (VM was already up-to-date)", assertions.VMRunCountIs(0)).
		Assert("Agent was launched", assertions.AgentLaunched()).
		Assert("User saw ready message", assertions.OutputContains("aivm is ready")).
		Assert("User saw agent launch step", assertions.OutputContains("Launching Claude Code (Anthropic) in VM")).
		Run()
}

// TestBareCommandCWDOutsideDevRoot verifies that DoLaunch returns an error
// when the working directory is not under DevRoot, and does NOT launch the agent.
//
// This protects against accidentally running an agent against a path that isn't
// mounted in the VM.
func TestBareCommandCWDOutsideDevRoot(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) — CWD outside DevRoot returns error before launching agent").
		Step("Pre-start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Override CWD to /tmp (outside DevRoot)", setCWD(h, "/tmp")).
		Step("Run: aivm (bare) — expect error", assertStepFails(
			actions.CLI(),
			"not under any configured VM mount",
		)).
		Assert("Agent was NOT launched (error occurred before dispatch)",
			assertions.AgentLaunchCount(0)).
		Run()
}

// TestBareCommandMultipleSessions verifies that each `aivm` invocation creates
// exactly one agent session. Running it twice results in two Launch calls.
func TestBareCommandMultipleSessions(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) invoked twice — two independent agent sessions").
		Step("First: aivm (bare)", actions.CLI()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("First agent session launched", assertions.AgentLaunchCount(1)).
		Step("Second: aivm (bare)", actions.CLI()).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Second agent session launched (total=2)", assertions.AgentLaunchCount(2)).
		Run()
}

// TestBareCommandWithTransitionState verifies that when a soft-rebuild
// transition is in progress, `aivm` bare routes the new session to the new VM
// profile rather than the legacy one.
//
//  1. Start VM (creates base image v1, primary profile).
//  2. Manually write a transition state pointing to a "-next" profile.
//  3. Register the "-next" profile so the factory can create its container.
//  4. Run `aivm` bare — DoLaunch must see the transition, switch to the new VM,
//     verify it is running, and launch the agent.
func TestBareCommandWithTransitionState(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) — transition active: agent routed to new VM profile").
		Step("Start primary VM (legacy profile)", actions.CLI("start")).
		Wait("Primary VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Prepare new VM and write transition state",
			prepareTransition(t, h)).
		Step("Run: aivm (bare) — must use new VM", actions.CLI()).
		Assert("Agent was launched (on the new VM)", assertions.AgentLaunched()).
		Run()
}

// ── helpers ────────────────────────────────────────────────────────────────

// setCWD returns a step that overrides h.App.Lifecycle.GetWorkDir to return dir,
// simulating running aivm from that path.
func setCWD(h *framework.Harness, dir string) framework.StepFunc {
	return func(_ context.Context, _ *framework.Harness) error {
		h.App.Lifecycle.GetWorkDir = func() (string, error) { return dir, nil }
		return nil
	}
}

// assertStepFails runs step and asserts that the error message contains wantSubstr.
// If step succeeds (no error), the test fails.
func assertStepFails(step framework.StepFunc, wantSubstr string) framework.StepFunc {
	return func(ctx context.Context, h *framework.Harness) error {
		err := step(ctx, h)
		if err == nil {
			return fmt.Errorf("expected step to fail with %q but it succeeded", wantSubstr)
		}
		if wantSubstr != "" && !contains(err.Error(), wantSubstr) {
			return fmt.Errorf("expected error containing %q, got: %v", wantSubstr, err)
		}
		return nil
	}
}

// prepareTransition starts the "-next" VM profile and writes a transition state
// file so that DoLaunch will route the next session to the new profile.
func prepareTransition(t *testing.T, h *framework.Harness) framework.StepFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		nextProfile := h.Profile + "-next"

		nextVM := h.GetOrCreateSecondaryVM(nextProfile)

		if err := nextVM.Start(ctx, vm.StartOptions{}); err != nil {
			return fmt.Errorf("start next VM: %w", err)
		}

		ts := &vm.TransitionState{
			LegacyProfile: h.Profile,
			NewProfile:    nextProfile,
		}
		if err := vm.SaveTransitionState(h.StateDir, ts); err != nil {
			return fmt.Errorf("save transition state: %w", err)
		}
		t.Logf("transition state written: legacy=%s new=%s", h.Profile, nextProfile)
		return nil
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
