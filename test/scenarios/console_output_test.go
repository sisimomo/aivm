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

// TestStartOutputReady verifies that `aivm start` prints "aivm is ready" to
// the user upon successful startup. This is the primary signal the user sees
// that the environment is ready to use.
func TestStartOutputReady(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm start prints 'aivm is ready'").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Output contains ready message", assertions.OutputContains("aivm is ready")).
		Assert("Output contains start step", assertions.OutputContains("Starting aivm")).
		Run()
}

// TestStopOutputMessage verifies that `aivm stop` prints "aivm stopped",
// confirming the user receives clear feedback that the VM was stopped.
func TestStopOutputMessage(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm stop prints 'aivm stopped'").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("Output contains stop message", assertions.OutputContains("aivm stopped")).
		Assert("Output does not show ready message for stop", assertions.OutputNotContains("aivm is ready")).
		Run()
}

// TestBootstrapSyncUpToDate verifies that a second `aivm start` on an
// already-bootstrapped VM shows "VM is up to date" — confirming users can tell
// when no work was done.
func TestBootstrapSyncUpToDate(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("second start prints 'VM is up to date'").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start again (up-to-date — no bootstrap needed)", actions.CLI("start")).
		Assert("Output shows up-to-date message", assertions.OutputContains("VM is up to date — skipping bootstrap")).
		Assert("Output shows ready", assertions.OutputContains("aivm is ready")).
		Run()
}

// TestBootstrapSyncNewPlugin verifies that when a new plugin is added,
// `aivm start` detects the config change, prompts to recreate, and after
// confirmation sets up the new plugin so users know work is happening.
func TestBootstrapSyncNewPlugin(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithPlugins("java"),
		framework.WithInteractive("y"), // answer "y" = recreate VM
	)

	h.Scenario("start with new plugin sets up the new plugin").
		Step("Start VM (first boot — installs java)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Add nodejs plugin", actions.AddPlugin("nodejs")).
		Step("Start again — config change detected, user confirms recreate", actions.CLI("start")).
		Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Output shows nodejs was set up",
			assertions.OutputContains("nodejs set up")).
		Assert("Output shows ready", assertions.OutputContains("aivm is ready")).
		Run()
}

// TestStatusCommandOutput verifies that `aivm status` produces the expected
// status table that users see when checking the VM state.
func TestStatusCommandOutput(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm status prints the status table").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run status command", actions.CLI("status")).
		Assert("Output contains status header", assertions.OutputContains("aivm status")).
		Assert("Output contains VM profile", assertions.OutputContains(h.Profile)).
		Assert("Output contains running indicator", assertions.OutputContains("Running")).
		Assert("Output contains MCPJungle line", assertions.OutputContains("MCPJungle")).
		Run()
}

// TestVMFileOutputCapture verifies that running a script in the VM and
// asserting on its output works end-to-end. This validates the RunOutput
// infrastructure used by assertions.VMRunOutput.
func TestVMFileOutputCapture(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("VM script output can be captured and asserted").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Script output contains expected text",
			assertions.VMRunOutput("echo 'hello from vm'", "hello from vm")).
		Assert("Script output contains hostname info",
			assertions.VMRunOutput("echo test-output", "test-output")).
		Run()
}
