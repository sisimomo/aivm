package e2e

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestClaudeProjectsPersistAcrossRecreate verifies that files written under the
// Claude projects mount in the VM survive aivm recreate on the host state dir.
func TestClaudeProjectsPersistAcrossRecreate(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	t.Parallel()
	skipUnlessDocker(t)

	vmHome := "/home/user"
	guestProjects := filepath.Join(vmHome, ".claude", "projects")
	markerName := "persist-marker.txt"

	h := framework.New(t, framework.WithProvider("claude"))
	hostMarker := filepath.Join(h.StateDir, ".claude", "projects", markerName)
	guestMarker := filepath.Join(guestProjects, markerName)

	h.Scenario("Claude projects dir persists across aivm recreate").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Write marker in VM Claude projects mount",
			actions.RunInVM("mkdir -p "+guestProjects+" && echo 'persist-me' > "+guestMarker)).
		Assert("Marker exists on host before recreate", assertions.HostFileExists(hostMarker)).
		Assert("Marker has expected content before recreate",
			assertions.HostFileContains(hostMarker, "persist-me")).
		Step("Recreate VM", actions.CLI("recreate", "--force")).
		Wait("VM is running after recreate", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete after recreate", assertions.BootstrapComplete()).
		Assert("Marker still exists on host after recreate", assertions.HostFileExists(hostMarker)).
		Assert("Marker content unchanged after recreate",
			assertions.HostFileContains(hostMarker, "persist-me")).
		Run()
}
