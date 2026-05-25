package e2e

// TestCLIEntryPoint demonstrates invoking AIVM through the real Cobra CLI
// entry point — the same path a user takes when running `aivm` in a terminal.
//
// Unlike the other tests in this package (which call cli.Do* directly), these
// tests use actions.CLI(...) which calls h.RunCLI → cli.NewRootCmd → cobra →
// command RunE → cli.Do*. This exercises:
//
//   - Cobra command routing
//   - Flag parsing (--force, --plugin, etc.)
//   - The full call stack from entry point to infrastructure
//
// The mock VM and no-op MCP stub are still used, so no real VMs or containers
// are required. The observable outcomes (bootstrap state files, VM status) are
// identical to what a real user would see.

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestCLIStartStop verifies that `aivm start` and `aivm stop` routed through
// the real Cobra entry point produce the same outcomes as calling DoStart/DoStop
// directly. This confirms the CLI wiring is correct end-to-end.
//
// Also covers the output assertions previously in TestStartOutputReady,
// TestStopOutputMessage, and TestStatusCommandOutput — all of which required
// the same VM boot, so they are folded here to avoid redundant boots.
func TestCLIStartStop(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm start → status → stop via CLI entry point").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Assert("User saw ready message", assertions.OutputContains("aivm is ready")).
		Assert("User saw starting step", assertions.OutputContains("Starting aivm")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm status", actions.CLI("status")).
		Assert("Status output contains header", assertions.OutputContains("aivm status")).
		Assert("Status output contains profile name", assertions.OutputContains(h.Profile)).
		Assert("Status output shows running", assertions.OutputContains("Running")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm stop", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Assert("User saw stop confirmation", assertions.OutputContains("aivm stopped")).
		Assert("Stop output does not show ready message", assertions.OutputNotContains("aivm is ready")).
		Run()
}

// TestCLIRecreateForceFlag verifies that `aivm recreate --force` completes
// without prompts, destroys the current VM, and bootstraps a new one.
// Tests that the --force flag is correctly parsed by the recreate subcommand.
func TestCLIRecreateForceFlag(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm recreate --force via CLI entry point").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Run: aivm recreate --force", actions.CLI("recreate", "--force")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after recreate", assertions.BootstrapComplete()).
		Run()
}
