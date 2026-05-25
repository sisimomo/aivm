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

// TestRecreateForce verifies that `aivm recreate --force` destroys the running
// VM and creates a fresh one with a full bootstrap — without interactive prompts.
//
//  1. Start the VM — bootstrap runs.
//  2. Run `aivm recreate --force` — VM is destroyed and recreated.
//  3. Bootstrap runs again on the new VM.
func TestRecreateForce(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("aivm recreate --force destroys and recreates VM").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Run: aivm recreate --force", actions.CLI("recreate", "--force")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after recreate", assertions.BootstrapComplete()).
		Assert("User saw ready message after recreate", assertions.OutputContains("aivm is ready")).
		Run()
}

// TestRecreateInteractiveAccepted verifies that `aivm recreate` in interactive
// mode proceeds when the user confirms with "y".
func TestRecreateInteractiveAccepted(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("y"),
	)

	h.Scenario("aivm recreate — user confirms, VM is recreated").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm recreate (interactive, answer y)", actions.CLI("recreate")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after recreate", assertions.BootstrapComplete()).
		Run()
}

// TestRecreateInteractiveDeclined verifies that `aivm recreate` in interactive
// mode cancels gracefully when the user answers "n".
func TestRecreateInteractiveDeclined(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithInteractive("n"),
	)

	h.Scenario("aivm recreate — user declines, VM is unchanged").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after start", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm recreate (interactive, answer n)", actions.CLI("recreate")).
		Assert("VM is still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Cancellation message shown", assertions.OutputContains("cancelled")).
		Run()
}

// TestRecreateWhileLaunchActive verifies that `aivm recreate --force` stops
// active sessions before destroying the VM.
func TestRecreateWhileLaunchActive(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithLaunchCommand("sleep 30"),
	)

	_, bgLaunch := actions.AsyncCLI()

	h.Scenario("aivm recreate --force with active sessions").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Launch agent (holds a session for 30s)", bgLaunch).
		Wait("Session active", conditions.SessionCount(1), 10*time.Second).
		Assert("Session is active", assertions.SessionCount(1)).
		Step("Run: aivm recreate --force", actions.CLI("recreate", "--force")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after recreate", assertions.BootstrapComplete()).
		Run()
}
