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

// TestStartSkipsBootstrapWhenUpToDate verifies that a second DoStart on an
// already-bootstrapped VM does not re-run any scripts.
//
//  1. First start: VM created, bootstrap runs the provider's required plugin,
//     bootstrap state is saved.
//  2. VM is left running.
//  3. Run counter is reset.
//  4. Second start with identical config: syncBootstrap detects nothing new and
//     skips — VM run count stays zero.
func TestStartSkipsBootstrapWhenUpToDate(t *testing.T) {
t.Parallel()
h := framework.New(t)

h.Scenario("second start skips bootstrap when config is unchanged").
Step("Start VM (first boot — bootstrap runs provider plugin)", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
Assert("At least one script ran during bootstrap", assertions.VMRunCountAtLeast(1)).
Step("Reset VM run counter", actions.ResetMockVMRunCount()).
Step("Reset output buffer", actions.ResetOutput()).
Step("Start VM again with identical config", actions.CLI("start")).
Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
Assert("No scripts ran — bootstrap was skipped", assertions.VMRunCountIs(0)).
Assert("Bootstrap state still complete", assertions.BootstrapComplete()).
Assert("User saw skip message", assertions.OutputContains("VM is up to date — skipping bootstrap")).
Run()
}

// TestConfigChangedPluginRecreatesVM verifies that when a new plugin is added
// to the config, syncBootstrap detects the hash change and prompts the user to
// recreate the VM. When confirmed, the VM is destroyed and recreated with the
// new config applied.
//
//  1. First start: bootstrap installs the provider plugin and "java".
//  2. Add "nodejs" to the plugin list.
//  3. Second start (answer "y"): hash changed → recreate prompt → VM recreated.
//  4. Marker files for all three plugins now exist in the recreated VM.
func TestConfigChangedPluginRecreatesVM(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithPlugins("java"),
framework.WithInteractive("y"), // answer "y" = recreate VM
)

h.Scenario("plugin added — user confirms VM recreation").
Step("Start VM (first boot — installs claude + java)", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
Assert("Claude marker file exists", assertions.VMFileExists("/tmp/.aivm_test_claude_installed")).
Assert("Java marker file exists", assertions.VMFileExists("/tmp/.aivm_test_java_installed")).
Step("Reset output buffer", actions.ResetOutput()).
Step("Add nodejs to plugin list", actions.ChangePlugins("java", "nodejs")).
Step("Start VM again — config change detected, user confirms recreate", actions.CLI("start")).
Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state updated", assertions.BootstrapComplete()).
Assert("NodeJS marker file exists", assertions.VMFileExists("/tmp/.aivm_test_nodejs_installed")).
Assert("User was warned about config change", assertions.StderrContains("config has changed")).
Assert("User saw VM recreated message", assertions.OutputContains("VM recreated")).
Run()
}

// TestStartRerunBootstrapAfterVersionMismatch verifies that a stale
// bootstrap state (wrong version) triggers a full re-bootstrap.
//
//  1. First start: bootstrap runs, state recorded.
//  2. Corrupt the state's version field to simulate an old format.
//  3. Reset run counter.
//  4. Second start: version mismatch triggers fullBootstrap → scripts run again.
func TestStartRerunBootstrapAfterVersionMismatch(t *testing.T) {
t.Parallel()
h := framework.New(t)

h.Scenario("stale bootstrap version triggers full re-bootstrap").
Step("Start VM (first boot)", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
Step("Corrupt bootstrap state version to simulate an upgrade", actions.CorruptBootstrapVersion()).
Step("Reset VM run counter", actions.ResetMockVMRunCount()).
Step("Reset output buffer", actions.ResetOutput()).
Step("Start VM again — version mismatch triggers full re-bootstrap", actions.CLI("start")).
Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
Assert("Re-bootstrap ran at least one script", assertions.VMRunCountAtLeast(1)).
Assert("Bootstrap state is valid again", assertions.BootstrapComplete()).
Assert("User saw reconcile header (force=false re-bootstrap)", assertions.OutputContains("Reconciling VM bootstrap")).
Assert("User saw completion message", assertions.OutputContains("Bootstrap complete!")).
Run()
}
