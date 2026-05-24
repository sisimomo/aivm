package e2e

// Tests for the multi-agent feature introduced in this branch.
//
// Multi-agent mode allows enabling more than one AI agent in aivm.yaml so that
// all of them are installed during bootstrap. The user picks which one to
// launch at runtime — either via agents.default (the default) or the --agent
// flag on the bare aivm command.
//
// Coverage in this file:
//
//  1. TestMultiAgent_BothAgentsBootstrapped — both enabled agents end up
//     installed in the VM after a single bootstrap run.
//
//  2. TestMultiAgent_AgentFlagLaunchesNonDefault — the --agent flag selects a
//     non-default enabled agent instead of the configured default.
//
//  3. TestMultiAgent_AgentFlagUnknownErrors — passing --agent with a name that
//     is not enabled returns a clear error and does not launch any agent.

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestMultiAgent_BothAgentsBootstrapped verifies that when two agents are
// enabled in aivm.yaml, a single bootstrap run installs both of them.
//
// Scenario:
//  1. Config: agents.default=claude, agents.define.claude.enable=true,
//     agents.define.opencode.enable=true.
//  2. aivm start — full bootstrap should install both claude and opencode.
//  3. Both binaries are present and runnable inside the VM.
func TestMultiAgent_BothAgentsBootstrapped(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithProvider("claude"),
		framework.WithExtraAgents("opencode"),
	)

	h.Scenario("two enabled agents are both installed during bootstrap").
		Step("Start VM with claude+opencode enabled", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 8*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Base image saved", assertions.BaseImageExists()).
		Assert("Claude binary is installed in VM",
			assertions.VMRunOutput("claude --version", "")).
		Assert("OpenCode binary is installed in VM",
			assertions.VMRunOutput("opencode --version", "")).
		Run()
}

// TestMultiAgent_AgentFlagLaunchesNonDefault verifies that passing --agent
// selects a specific enabled agent instead of the default.
//
// Scenario:
//  1. Config: default=claude, opencode also enabled.
//  2. aivm start — bootstrap installs both agents.
//  3. aivm --agent opencode — OpenCode is launched, not Claude.
func TestMultiAgent_AgentFlagLaunchesNonDefault(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithProvider("claude"),
		framework.WithExtraAgents("opencode"),
	)

	h.Scenario("--agent flag launches non-default enabled agent").
		Step("Start VM (bootstrap both agents)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 8*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm --agent opencode", actions.CLI("--agent", "opencode")).
		Assert("Agent was launched", assertions.AgentLaunched()).
		Assert("Exactly one launch happened", assertions.AgentLaunchCount(1)).
		Assert("OpenCode was the launched agent",
			assertions.OutputContains("Launching OpenCode CLI in VM")).
		Assert("Claude was NOT launched",
			assertions.OutputNotContains("Launching Claude Code")).
		Run()
}

// TestMultiAgent_AgentFlagUnknownErrors verifies that --agent with a name that
// is not in the enabled set returns a clear error without launching anything.
//
// Scenario:
//  1. Config: single claude agent enabled.
//  2. aivm start — bootstrap runs.
//  3. aivm --agent mystery — should fail with "is not enabled".
//  4. No agent was launched.
func TestMultiAgent_AgentFlagUnknownErrors(t *testing.T) {
	t.Parallel()
	h := framework.New(t) // default: only claude enabled

	h.Scenario("--agent with unknown name returns error, no agent launched").
		Step("Pre-start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm --agent mystery (expect error)", assertStepFails(
			actions.CLI("--agent", "mystery"),
			"is not enabled",
		)).
		Assert("No agent was launched", assertions.AgentLaunchCount(0)).
		Run()
}
