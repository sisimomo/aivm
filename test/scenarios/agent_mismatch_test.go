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

// TestAgentMismatchRecreateVM verifies that when the active provider changes,
// the user is prompted to recreate the VM. When confirmed, the VM is destroyed
// and recreated with only the new provider.
//
// Scenario:
//  1. Start VM with claude provider — bootstrap installs the claude plugin.
//  2. Switch config to copilot provider.
//  3. Start VM again (interactive mode, answer "y"):
//     syncBootstrap detects the config change and asks whether to recreate.
//  4. Answer "y" → VM is destroyed and recreated with only the copilot plugin.
//  5. Claude plugin marker is no longer present; copilot marker is.
func TestAgentMismatchRecreateVM(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithInteractive("y"), // answer "y" = recreate VM
)

h.Scenario("agent config changed — user confirms VM recreation").
Step("Start VM with claude provider (first boot)", actions.CLI("start")).
Wait("VM is running with claude", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state records claude provider",
assertions.BootstrapStateProviderIs("claude")).
Assert("Claude marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_claude_installed")).
Step("Switch config to copilot provider", actions.ChangeProvider("copilot")).
Step("Start VM again — config change detected, user confirms recreate",
actions.CLI("start")).
Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state updated to copilot provider",
assertions.BootstrapStateProviderIs("copilot")).
Assert("Copilot marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_copilot_installed")).
Assert("User was warned about config change",
assertions.StderrContains("config has changed")).
Assert("User saw VM recreated message",
assertions.OutputContains("VM recreated")).
Run()
}

// TestConfigChangedDeclined verifies that when the agent config changes and
// the user declines recreation, the VM keeps running with the old config.
//
// Scenario:
//  1. Start VM with claude provider — bootstrap installs the claude plugin.
//  2. Switch config to copilot provider.
//  3. Start VM again (interactive mode, answer "n"):
//     syncBootstrap detects the config change and asks whether to recreate.
//  4. Answer "n" → VM continues as-is; no scripts run; bootstrap state unchanged.
func TestConfigChangedDeclined(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithInteractive("n"), // answer "n" = keep VM as-is
)

h.Scenario("config changed — user declines recreation, VM continues with old config").
Step("Start VM with claude provider (first boot)", actions.CLI("start")).
Wait("VM is running with claude", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state records claude provider",
assertions.BootstrapStateProviderIs("claude")).
Step("Switch config to copilot provider", actions.ChangeProvider("copilot")).
Step("Reset run counter", actions.ResetMockVMRunCount()).
Step("Reset output buffer", actions.ResetOutput()).
Step("Start VM again — config change detected, user declines recreate",
actions.CLI("start")).
Assert("VM still running (was not destroyed)", assertions.VMStatus(vm.StatusRunning)).
Assert("No scripts ran — VM was not recreated", assertions.VMRunCountIs(0)).
Assert("Bootstrap state still records claude (unchanged)",
assertions.BootstrapStateProviderIs("claude")).
Assert("User was warned about config change",
assertions.StderrContains("config has changed")).
Assert("User saw continue message",
assertions.OutputContains("Continuing without applying config changes")).
Run()
}
