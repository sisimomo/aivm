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

// TestIntegrationRunsForActiveAgent verifies that when rtk is installed and
// claude is the active agent, the rtk→claude integration executes and its
// marker file appears in the VM.
func TestIntegrationRunsForActiveAgent(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithPlugins("rtk"),
framework.WithProvider("claude"),
)

h.Scenario("rtk→claude integration runs when rtk installed and claude active").
Step("Start VM", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap complete", assertions.BootstrapComplete()).
Assert("rtk→claude marker file exists in VM",
assertions.VMFileExists("/tmp/.aivm_test_integ_rtk_claude")).
Assert("User saw integration step in output", assertions.OutputContains("Integration: rtk:claude")).
Run()
}

// TestIntegrationSkipsForInactiveAgent verifies that when copilot is active,
// the rtk→claude integration does NOT run (even if rtk is installed).
func TestIntegrationSkipsForInactiveAgent(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithPlugins("rtk"),
framework.WithProvider("copilot"),
)

h.Scenario("rtk→claude integration skipped when copilot is active").
Step("Start VM with copilot provider", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap complete", assertions.BootstrapComplete()).
Assert("rtk→copilot marker file exists in VM",
assertions.VMFileExists("/tmp/.aivm_test_integ_rtk_copilot")).
Assert("rtk→claude marker file is absent in VM",
assertions.VMFileAbsent("/tmp/.aivm_test_integ_rtk_claude")).
Assert("rtk:claude integration step not shown to user",
assertions.OutputNotContains("Integration: rtk:claude")).
Run()
}

// TestIntegrationNotRunWithoutPlugin verifies that when rtk is NOT installed,
// rtk integrations do not run. The agent-only MCP integration still runs.
func TestIntegrationNotRunWithoutPlugin(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithProvider("claude"),
// no rtk in Plugins
)

h.Scenario("rtk integration skipped when rtk is not installed").
Step("Start VM (no rtk plugin)", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap complete", assertions.BootstrapComplete()).
Assert("mcp→claude marker file exists (agent-only integration always runs)",
assertions.VMFileExists("/tmp/.aivm_test_integ_mcpjungle_claude")).
Assert("rtk→claude marker file is absent in VM",
assertions.VMFileAbsent("/tmp/.aivm_test_integ_rtk_claude")).
Run()
}

// TestIntegrationIsIdempotent verifies that integrations are not re-run on a
// second start when the config is unchanged. The hash matches on second start,
// so the upToDateStep fires and no scripts run.
func TestIntegrationIsIdempotent(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithPlugins("rtk"),
framework.WithProvider("claude"),
)

h.Scenario("integration runs once and is idempotent on second start").
Step("Start VM (first boot — integration runs)", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap complete", assertions.BootstrapComplete()).
Assert("marker file created by integration", assertions.VMFileExists("/tmp/.aivm_test_integ_rtk_claude")).
Step("Reset VM run counter", actions.ResetMockVMRunCount()).
Step("Start VM again with identical config", actions.CLI("start")).
Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
Assert("No scripts ran on second start (hash unchanged, bootstrap skipped)", assertions.VMRunCountIs(0)).
Assert("marker file still exists in VM", assertions.VMFileExists("/tmp/.aivm_test_integ_rtk_claude")).
Run()
}

// TestIntegrationRunsWhenPluginNewlyInstalled verifies that adding rtk after
// the initial boot causes the rtk integration to run on the next start.
// The agent-only MCP integration runs on first boot regardless.
func TestIntegrationRunsWhenPluginNewlyInstalled(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithProvider("claude"),
// no rtk initially
)

h.Scenario("rtk integration runs when rtk is added after initial boot").
Step("Start VM without rtk", actions.CLI("start")).
Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap complete", assertions.BootstrapComplete()).
Assert("mcp→claude marker file exists (agent-only)",
assertions.VMFileExists("/tmp/.aivm_test_integ_mcpjungle_claude")).
Assert("rtk→claude marker file is absent (rtk not yet installed)",
assertions.VMFileAbsent("/tmp/.aivm_test_integ_rtk_claude")).
Step("Add rtk to plugin list", actions.ChangePlugins("rtk")).
Step("Reset run counter before second start", actions.ResetMockVMRunCount()).
Step("Start VM again with rtk now enabled", actions.CLI("start")).
Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
Assert("Scripts ran: rtk setup + integration", assertions.VMRunCountAtLeast(1)).
Assert("rtk→claude marker file now exists", assertions.VMFileExists("/tmp/.aivm_test_integ_rtk_claude")).
Run()
}
