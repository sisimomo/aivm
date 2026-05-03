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

// TestStopDestroyRestart exercises the complete VM lifecycle:
//
//  1. Start → Running.
//  2. Stop → Stopped (disk preserved, bootstrap state kept).
//  3. Start → Running (VM resumed, bootstrap sync detects nothing new).
//  4. Destroy → NotFound.
//  5. Start → Running (fresh VM, bootstrap runs again).
func TestStopDestroyRestart(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("full VM lifecycle: start → stop → resume → destroy → recreate").
		Step("Start VM (first boot — bootstrap runs)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Base image saved", assertions.BaseImageExists()).
		Assert("Bootstrap ran: user saw 'Bootstrapping VM'", assertions.OutputContains("Bootstrapping VM")).
		Assert("Bootstrap ran: user saw 'Bootstrap complete!'", assertions.OutputContains("Bootstrap complete!")).
		Assert("Start finished: user saw 'aivm is ready'", assertions.OutputContains("aivm is ready")).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Assert("Stop finished: user saw 'aivm stopped'", assertions.OutputContains("aivm stopped")).
		Step("Resume VM via Start (no bootstrap — VM was not destroyed)", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("VM is running after resume", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still intact", assertions.BootstrapComplete()).
		Step("Destroy VM entirely", actions.CLI("destroy")).
		Wait("VM is gone", conditions.VMStatus(vm.StatusNotFound), 2*time.Minute).
		Assert("VM is not found", assertions.VMStatus(vm.StatusNotFound)).
		Assert("Destroy finished: user saw 'VM destroyed'", assertions.OutputContains("VM destroyed")).
		Step("Recreate VM from scratch via Start", actions.CLI("start")).
		Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap ran again on new VM", assertions.BootstrapComplete()).
		Assert("New base image saved", assertions.BaseImageExists()).
		Run()
}

// TestBootstrapCommandForce verifies that `aivm bootstrap --force` re-runs all
// plugins on an already-bootstrapped VM.
//
//  1. Start VM — bootstrap runs.
//  2. Reset run counter.
//  3. DoBootstrap(force=true) — all plugins run again regardless of state.
func TestBootstrapCommandForce(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("bootstrap --force re-runs all plugins on an already-bootstrapped VM").
		Step("Start VM (first boot — bootstrap installs plugins)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run bootstrap --force (re-runs everything)", actions.CLI("bootstrap", "--force")).
		Assert("Scripts ran again (force=true bypasses up-to-date check)",
			assertions.VMRunCountAtLeast(1)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Assert("User saw force-bootstrap header", assertions.OutputContains("Bootstrapping VM")).
		Assert("User saw completion message", assertions.OutputContains("Bootstrap complete!")).
		Assert("User saw provider plugin step", assertions.OutputContains("Plugin: claude")).
		Run()
}

// TestBootstrapCommandSinglePlugin verifies that `aivm bootstrap --plugin java`
// runs only the specified plugin on an already-running VM.
//
//  1. Start VM — bootstrap runs (installs claude plugin).
//  2. Reset run counter.
//  3. DoBootstrap(plugin="java") — only the java plugin runs.
func TestBootstrapCommandSinglePlugin(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("bootstrap --plugin runs only the specified plugin").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Reset run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run bootstrap --plugin java (only java plugin)", actions.CLI("bootstrap", "--plugin", "java")).
		Assert("At least one script ran (the java plugin's steps)", assertions.VMRunCountAtLeast(1)).
		Assert("User saw java plugin step", assertions.OutputContains("Plugin: java")).
		Assert("User saw java installed message", assertions.OutputContains("java set up")).
		Run()
}

// TestBootstrapCommandSync verifies that `aivm bootstrap` (no flags) on an
// already-up-to-date VM logs "VM is up to date" and runs no scripts.
//
//  1. Start VM — bootstrap runs.
//  2. Reset run counter.
//  3. DoBootstrap(force=false, plugin="") — detects up-to-date, skips.
func TestBootstrapCommandSync(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("bootstrap sync on up-to-date VM — no scripts run").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Reset run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run bootstrap (sync — no flags)", actions.CLI("bootstrap")).
		Assert("No scripts ran — VM was already up to date", assertions.VMRunCountIs(0)).
		Assert("User saw up-to-date message", assertions.OutputContains("VM is up to date — skipping bootstrap")).
		Run()
}
