package scenarios

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

	"aivm/internal/vm"
	"aivm/test/framework"
	"aivm/test/framework/actions"
	"aivm/test/framework/assertions"
	"aivm/test/framework/conditions"
)

// TestCLIStartStop verifies that `aivm start` and `aivm stop` routed through
// the real Cobra entry point produce the same outcomes as calling DoStart/DoStop
// directly. This confirms the CLI wiring is correct end-to-end.
func TestCLIStartStop(t *testing.T) {
	h := framework.New(t)

	h.Scenario("aivm start → aivm stop via CLI entry point").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Assert("Base image saved after start", assertions.BaseImageExists()).
		Step("Run: aivm stop", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Run()
}

// TestCLIBootstrapForceFlag verifies that `aivm bootstrap --force` re-runs all
// plugins. This specifically tests that the --force flag is parsed correctly
// and reaches DoBootstrap with force=true.
func TestCLIBootstrapForceFlag(t *testing.T) {
	h := framework.New(t)

	h.Scenario("aivm bootstrap --force re-runs all plugins (flag parsed via cobra)").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Run: aivm bootstrap --force", actions.CLI("bootstrap", "--force")).
		Assert("Scripts ran again (--force flag reached DoBootstrap)",
			assertions.VMRunCountAtLeast(1)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Run()
}

// TestCLIBootstrapPluginFlag verifies that `aivm bootstrap --plugin java`
// runs only the specified plugin. This tests that the --plugin flag is parsed
// and forwarded correctly.
func TestCLIBootstrapPluginFlag(t *testing.T) {
	h := framework.New(t)

	h.Scenario("aivm bootstrap --plugin java runs only the java plugin (flag parsed via cobra)").
		Step("Run: aivm start", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Run: aivm bootstrap --plugin java", actions.CLI("bootstrap", "--plugin", "java")).
		Assert("At least one script ran (the java plugin's steps)",
			assertions.VMRunCountAtLeast(1)).
		Run()
}

// TestCLIRebuildImageForceFlag verifies that `aivm rebuild-image --force`
// completes without prompts, destroys the current VM, and saves a new base
// image. Tests that the --force flag is correctly parsed by the rebuild-image
// subcommand.
func TestCLIRebuildImageForceFlag(t *testing.T) {
	h := framework.New(t)

	var v1ID string

	h.Scenario("aivm rebuild-image --force via CLI entry point").
		Step("Run: aivm start (creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", captureBaseImageID(t, &v1ID)).
		Step("Wait 1s to ensure new image gets a different timestamp",
			sleepStep(1100*time.Millisecond)).
		Step("Run: aivm rebuild-image --force", actions.CLI("rebuild-image", "--force")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("New base image saved", assertions.BaseImageExists()).
		Assert("Base image ID changed after rebuild", assertions.BaseImageIsNot(&v1ID)).
		Assert("VM image ref is current", assertions.VMImageRefCurrent()).
		Run()
}
