package scenarios

import (
	"testing"
	"time"

	"aivm/internal/vm"
	"aivm/test/framework"
	"aivm/test/framework/actions"
	"aivm/test/framework/assertions"
	"aivm/test/framework/conditions"
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
	h := framework.New(t)

	h.Scenario("second start skips bootstrap when config is unchanged").
		Step("Start VM (first boot — bootstrap runs provider plugin)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Assert("At least one script ran during bootstrap", assertions.VMRunCountAtLeast(1)).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Start VM again with identical config", actions.Start()).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("No scripts ran — bootstrap was skipped", assertions.VMRunCountIs(0)).
		Assert("Bootstrap state still complete", assertions.BootstrapComplete()).
		Run()
}

// TestStartInstallsNewPluginsIncrementally verifies that when a new plugin is
// added to the config, DoStart installs only that plugin rather than
// re-bootstrapping everything.
//
//  1. First start: bootstrap installs the provider plugin and "java".
//  2. Reset run counter.
//  3. Add "nodejs" to the plugin list.
//  4. Second start: only "nodejs" is installed (one script run).
//  5. Bootstrap state now includes all three plugins.
func TestStartInstallsNewPluginsIncrementally(t *testing.T) {
	h := framework.New(t,
		framework.WithPlugins("java"),
	)

	h.Scenario("second start installs only newly-added plugins").
		Step("Start VM (first boot — installs claude + java)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Assert("Bootstrap includes claude and java",
			assertions.BootstrapStateContainsPlugins("claude", "java")).
		Step("Reset VM run counter after first boot", actions.ResetMockVMRunCount()).
		Step("Add nodejs to plugin list", actions.ChangePlugins("java", "nodejs")).
		Step("Start VM again — only nodejs should be installed", actions.Start()).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("At least one script ran for the new nodejs plugin", assertions.VMRunCountAtLeast(1)).
		Assert("Bootstrap state now includes all three plugins",
			assertions.BootstrapStateContainsPlugins("claude", "java", "nodejs")).
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
	h := framework.New(t)

	h.Scenario("stale bootstrap version triggers full re-bootstrap").
		Step("Start VM (first boot)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Corrupt bootstrap state version to simulate an upgrade", actions.CorruptBootstrapVersion()).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Start VM again — version mismatch triggers full re-bootstrap", actions.Start()).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("Re-bootstrap ran at least one script", assertions.VMRunCountAtLeast(1)).
		Assert("Bootstrap state is valid again", assertions.BootstrapComplete()).
		Run()
}
