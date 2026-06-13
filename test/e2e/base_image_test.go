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

func TestDestroyKeepBase(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("destroy --keep-base retains base for fast recreate").
		Step("Start VM", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Destroy keeping base", actions.CLI("destroy", "--keep-base")).
		Assert("bootstrap state preserved", assertions.StateFileExists("bootstrap-state.json")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Fast start from base", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM running after fast start", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Assert("Full bootstrap not rerun", assertions.OutputNotContains("Bootstrapping VM")).
		Run()
}

func TestRecreateFast(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("recreate --fast restores from base without full bootstrap").
		Step("Start VM", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Fast recreate", actions.CLI("recreate", "--fast")).
		Wait("VM running after fast recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Assert("Full bootstrap not rerun", assertions.OutputNotContains("Bootstrapping VM")).
		Run()
}

func TestBootstrapRefreshAccepted(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithBootstrapRefreshDays(30),
		framework.WithInteractive("y"),
	)

	h.Scenario("stale bootstrap — user accepts full refresh").
		Step("Start VM", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Backdate bootstrap-at by 31 days", actions.SetBootstrapDaysAgo(31)).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM — bootstrap refresh prompt, user accepts", actions.CLI("start")).
		Wait("VM running after refresh", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Full bootstrap ran", assertions.OutputContains("Bootstrapping VM")).
		Assert("Bootstrap state valid after refresh", assertions.BootstrapComplete()).
		Run()
}

func TestBootstrapRefreshDeclined_StoppedVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithBootstrapRefreshDays(30),
		framework.WithInteractive("n"),
	)

	h.Scenario("stale bootstrap — stopped VM, user declines refresh, fast recreate").
		Step("Start VM", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Step("Backdate bootstrap-at by 31 days", actions.SetBootstrapDaysAgo(31)).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM — bootstrap refresh prompt, user declines", actions.CLI("start")).
		Wait("VM running after fast recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("VM running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still valid", assertions.BootstrapComplete()).
		Assert("Full bootstrap not rerun", assertions.OutputNotContains("Bootstrapping VM")).
		Run()
}
