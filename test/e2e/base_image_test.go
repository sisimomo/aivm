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
		Step("Fast start from base", actions.CLI("start")).
		Wait("VM running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap skipped", assertions.OutputContains("skipping bootstrap")).
		Run()
}
