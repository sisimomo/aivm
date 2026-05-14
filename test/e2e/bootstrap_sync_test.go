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

// TestStartSkipsBootstrapWhenUpToDate verifies that a second DoStart on an
// already-bootstrapped VM does not re-run any scripts.
//
//  1. First start: VM created, bootstrap runs the provider's required plugin,
//     bootstrap state is saved.
//  2. VM is left running.
//  3. Second start with identical config: syncBootstrap detects nothing new and
//     skips — "VM is up to date" is shown.
func TestStartSkipsBootstrapWhenUpToDate(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("second start skips bootstrap when config is unchanged").
		Step("Start VM (first boot — bootstrap runs provider plugin)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Assert("Bootstrap ran on first boot", assertions.OutputContains("Bootstrap complete!")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM again with identical config", actions.CLI("start")).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still complete", assertions.BootstrapComplete()).
		Assert("User saw skip message", assertions.OutputContains("VM is up to date — skipping bootstrap")).
		Run()
}

// TestConfigChangedPluginRecreatesVM verifies that when a new plugin is added
// to the config, syncBootstrap detects the hash change and prompts the user to
// recreate the VM. When confirmed, the VM is destroyed and recreated with the
// new config applied.
//
//  1. First start: bootstrap installs the provider plugin and "awscli".
//  2. Add "mise" to the plugin list.
//  3. Second start (answer "y"): hash changed → recreate prompt → VM recreated.
//  4. Marker files for all three plugins now exist in the recreated VM.
func TestConfigChangedPluginRecreatesVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t,
		framework.WithPlugins("awscli"),
		framework.WithInteractive("y"), // answer "y" = recreate VM
	)

	h.Scenario("plugin added — user confirms VM recreation").
		Step("Start VM (first boot — installs claude + awscli)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Assert("AWS CLI installed in VM", assertions.VMRunOutput("aws --version", "aws-cli")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Add mise to plugin list", actions.ChangePlugins("awscli", "mise")).
		Step("Start VM again — config change detected, user confirms recreate", actions.CLI("start")).
		Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state updated", assertions.BootstrapComplete()).
		Assert("mise installed in VM", assertions.VMRunOutput("mise --version", "")).
		Assert("User was warned about config change", assertions.StderrContains("config has changed")).
		Assert("User saw VM recreated message", assertions.OutputContains("VM recreated")).
		Run()
}

// TestStartRerunBootstrapAfterVersionMismatch verifies that a stale
// bootstrap state (wrong version) triggers a full re-bootstrap.
//
//  1. First start: bootstrap runs, state recorded.
//  2. Corrupt the state's version field to simulate an old format.
//  3. Second start: version mismatch triggers fullBootstrap → scripts run again.
func TestStartRerunBootstrapAfterVersionMismatch(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("stale bootstrap version triggers full re-bootstrap").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Corrupt bootstrap state version to simulate an upgrade", actions.CorruptBootstrapVersion()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM again — version mismatch triggers full re-bootstrap", actions.CLI("start")).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Re-bootstrap ran", assertions.OutputContains("Bootstrap complete!")).
		Assert("Bootstrap state is valid again", assertions.BootstrapComplete()).
		Assert("User saw reconcile header (force=false re-bootstrap)", assertions.OutputContains("Reconciling VM bootstrap")).
		Assert("User saw completion message", assertions.OutputContains("Bootstrap complete!")).
		Run()
}

// TestVMEnvChangedAppliesInPlace verifies that changing vm.env triggers the
// lightweight envChangedStep — re-applying the env file without recreating the VM.
//
//  1. First start: VM bootstrapped, env_hash recorded in bootstrap state.
//  2. Add an env var to vm.env.
//  3. Second start: envChangedStep detects env change, applies it via SSH.
//  4. Env file exists in the VM.
//  5. EnvHash updated in bootstrap state.
func TestVMEnvChangedAppliesInPlace(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	h.Scenario("vm.env changed — applied in-place without VM recreation").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Change vm.env", actions.ChangeVMEnv(map[string]string{"AIVM_TEST_VAR": "hello"})).
		Step("Start VM again — env changed, no VM recreation", actions.CLI("start")).
		Assert("VM still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Bootstrap state still complete", assertions.BootstrapComplete()).
		Assert("Bootstrap state records env_hash", assertions.BootstrapEnvHashSet()).
		Assert("User saw env update message", assertions.OutputContains("Environment variables updated")).
		Assert("Env file exists in VM", assertions.VMFileExists("/etc/profile.d/aivm-user-env.sh")).
		Assert("Env file contains the variable", assertions.VMRunOutput(
			"grep AIVM_TEST_VAR /etc/profile.d/aivm-user-env.sh",
			"AIVM_TEST_VAR",
		)).
		Run()
}
