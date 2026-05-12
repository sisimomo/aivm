package e2e

// Tests for the bare `aivm` command — the most frequently used invocation.
//
// Running `aivm` with no arguments calls DoStart (ensure VM is running with
// bootstrap complete) followed by DoLaunch (start an AI agent session in the
// current working directory). This file verifies every meaningful code path
// through that two-step flow.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
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

// TestBareCommandResume verifies the second-most-common flow, and also covers
// the CWD-outside-DevRoot error path — both cases pre-start the VM, so they
// share a single harness to avoid an extra bootstrap cycle.
//
//  1. VM is already running from a previous session (simulate with `aivm start`).
//  2. User runs `aivm` — DoStart detects the VM is already up and skips bootstrap.
//  3. DoLaunch starts the agent directly.
//  4. User runs `aivm` from /tmp (outside DevRoot) — DoLaunch returns an error
//     before dispatching; agent is NOT launched a second time.
func TestBareCommandResumeAndCWDValidation(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm (bare) — resume + CWD outside DevRoot error, shared VM boot").
		Step("Pre-start VM so it is already running", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm (bare) — VM already up", actions.CLI()).
		Assert("VM is still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Agent was launched", assertions.AgentLaunched()).
		Assert("Agent launched exactly once", assertions.AgentLaunchCount(1)).
		Assert("User saw ready message", assertions.OutputContains("aivm is ready")).
		Assert("User saw agent launch step", assertions.OutputContains("Launching Claude Code (Anthropic) in VM")).
		Step("Override CWD to /tmp (outside DevRoot)", actions.SetWorkDir("/tmp")).
		Step("Run: aivm (bare) — expect CWD error", assertStepFails(
			actions.CLI(),
			"not under any configured VM mount",
		)).
		Assert("Agent was NOT launched again (error before dispatch)",
			assertions.AgentLaunchCount(1)).
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

// TestBareCommandWithTransitionState is removed — the legacy VM / transition
// state feature has been deleted.

// ── helpers ────────────────────────────────────────────────────────────────

// assertStepFails runs step and asserts that the combined CLI output contains
// wantSubstr. If step succeeds (no error), the test fails.
func assertStepFails(step framework.StepFunc, wantSubstr string) framework.StepFunc {
	return func(ctx context.Context, h *framework.Harness) error {
		err := step(ctx, h)
		if err == nil {
			return fmt.Errorf("expected step to fail with %q but it succeeded", wantSubstr)
		}
		if wantSubstr != "" {
			output := h.Output.Stdout() + h.Output.Stderr()
			if !strings.Contains(output, wantSubstr) {
				return fmt.Errorf("expected output containing %q\ngot stdout: %s\ngot stderr: %s",
					wantSubstr, h.Output.Stdout(), h.Output.Stderr())
			}
		}
		return nil
	}
}
