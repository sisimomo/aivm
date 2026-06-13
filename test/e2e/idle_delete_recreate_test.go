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

// TestIdleDeleteRecreate verifies the full two-phase idle lifecycle:
//
//  1. Start the VM and launch the idle monitor in-process.
//  2. Phase 1: no sessions → monitor stops the VM after the idle timeout.
//  3. Phase 2: VM stays suspended → monitor deletes it after the delete timeout.
//  4. Start again — VM is fast-recreated from the saved base image; bootstrap
//     plugins are not rerun.
//
// Both timeouts are very short (10s each) so the full cycle completes quickly.
func TestIdleDeleteRecreate(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithCPUs(2),
		framework.WithMemoryGiB(4),
		framework.WithIdleTimeout(3*time.Second),
		framework.WithDeleteTimeout(3*time.Second),
		framework.WithPollInterval(1*time.Second),
	)

	h.Scenario("idle → stop → delete → recreate").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		// Phase 1: idle timeout elapses → VM suspended.
		Wait("VM auto-stopped (Phase 1)", conditions.VMStatus(vm.StatusStopped), 60*time.Second).
		Wait("vm-stopped-at marker written", conditions.StateFileExists("vm-stopped-at"), 5*time.Second).
		// Phase 2: delete timeout elapses → VM destroyed.
		Wait("VM auto-deleted (Phase 2)", conditions.VMStatus(vm.StatusNotFound), 60*time.Second).
		Assert("VM is gone", assertions.VMStatus(vm.StatusNotFound)).
		Assert("Bootstrap state preserved", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Fast recreate VM via Start", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM is running after fast recreate", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Assert("Full bootstrap not rerun", assertions.OutputNotContains("Bootstrapping VM")).
		Run()
}
