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
		Run()
}

// TestSSHAutoStart verifies that `aivm ssh` starts the VM automatically when
// it is not running, bootstraps it if needed, then opens the shell.
//
//  1. No VM running.
//  2. aivm ssh — auto-starts the VM, runs bootstrap, opens SSH shell.
func TestSSHAutoStart(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm ssh starts and bootstraps VM when not running").
		Step("Run: aivm ssh (VM not yet running)", actions.CLI("ssh")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap completed automatically", assertions.BootstrapComplete()).
		Assert("User saw ready message", assertions.OutputContains("aivm is ready")).
		Run()
}

// TestSSHOopensCWD verifies that aivm ssh lands in the host's current working
// directory (mirrored into the VM via bind mounts).
func TestSSHOopensCWD(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm ssh opens current working directory").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("SSH: print pwd", actions.CLIWithStdin("pwd\nexit\n", "ssh")).
		Assert("SSH lands in mounted dev root", assertions.OutputContains("/dev")).
		Assert("SSH lands in test run directory", assertions.OutputContains("test-runs")).
		Run()
}
