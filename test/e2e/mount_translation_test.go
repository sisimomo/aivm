package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestSSH_RemappedMountWorkDir verifies that aivm ssh translates the host CWD
// to the guest mountPoint when location and mountPoint differ.
func TestSSH_RemappedMountWorkDir(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	t.Parallel()
	skipUnlessDocker(t)

	devRoot := t.TempDir()
	nestedDir := filepath.Join(devRoot, "nested")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	h := framework.New(t, framework.WithRemappedMount(devRoot, "/work"))

	h.Scenario("aivm ssh translates remapped mount CWD to guest path").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Override CWD to nested dir under host mount", actions.SetWorkDir(nestedDir)).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("SSH: print pwd", actions.CLIWithStdin("pwd\nexit\n", "ssh")).
		Assert("SSH lands in remapped guest path", assertions.OutputContains("/work/nested")).
		Run()
}
