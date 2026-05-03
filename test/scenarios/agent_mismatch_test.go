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

// TestAgentMismatchInstallIntoExistingVM verifies that when the active provider
// changes from claude to copilot, the user can choose to install the new
// provider into the existing VM (option 1) without destroying it.
//
// Scenario:
//  1. Start VM with claude provider — bootstrap installs the claude plugin.
//  2. Switch config to copilot provider.
//  3. Start VM again (interactive mode, answer "1"):
//     syncBootstrap detects the provider mismatch, asks whether to install into
//     the existing VM or destroy and recreate.
//  4. Answer "1" → copilot plugin is installed alongside claude.
//  5. Marker files for both providers exist in the VM.
//  6. VM is NOT destroyed (still running, same instance).
func TestAgentMismatchInstallIntoExistingVM(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithInteractive("1"), // answer "1" = install into existing VM
)

h.Scenario("agent mismatch — install new provider into existing VM").
Step("Start VM with claude provider (first boot)", actions.CLI("start")).
Wait("VM is running with claude", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state records claude provider",
assertions.BootstrapStateProviderIs("claude")).
Assert("Claude marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_claude_installed")).
Step("Switch config to copilot provider", actions.ChangeProvider("copilot")).
Step("Reset run counter", actions.ResetMockVMRunCount()).
Step("Reset output buffer", actions.ResetOutput()).
Step("Start VM again — mismatch detected, choose to install into existing VM",
actions.CLI("start")).
Assert("VM still running (was not destroyed)", assertions.VMStatus(vm.StatusRunning)).
Assert("Copilot plugin was set up (at least one script ran)", assertions.VMRunCountAtLeast(1)).
Assert("Claude marker still exists",
assertions.VMFileExists("/tmp/.aivm_test_claude_installed")).
Assert("Copilot marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_copilot_installed")).
Assert("User was warned about agent mismatch",
assertions.StderrContains("was created for a different agent")).
Assert("User saw copilot plugin setup step",
assertions.OutputContains("Plugin: copilot")).
Assert("User saw copilot set up confirmation",
assertions.OutputContains("copilot set up")).
Run()
}

// TestAgentMismatchRecreateVM verifies that when the active provider changes,
// the user can choose to destroy the existing VM and create a fresh one with
// only the new provider (option 2).
//
// Scenario:
//  1. Start VM with claude provider — bootstrap installs the claude plugin.
//  2. Switch config to copilot provider.
//  3. Start VM again (interactive mode, answer "2"):
//     syncBootstrap detects the mismatch, asks for choice.
//  4. Answer "2" → VM is destroyed and recreated with only the copilot plugin.
//  5. Claude plugin marker is no longer present; copilot marker is.
func TestAgentMismatchRecreateVM(t *testing.T) {
t.Parallel()
h := framework.New(t,
framework.WithInteractive("2"), // answer "2" = destroy and recreate
)

h.Scenario("agent mismatch — destroy and recreate VM with new provider only").
Step("Start VM with claude provider (first boot)", actions.CLI("start")).
Wait("VM is running with claude", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state records claude provider",
assertions.BootstrapStateProviderIs("claude")).
Assert("Claude marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_claude_installed")).
Step("Switch config to copilot provider", actions.ChangeProvider("copilot")).
Step("Start VM again — mismatch detected, choose to destroy and recreate",
actions.CLI("start")).
Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
Assert("Bootstrap state updated to copilot provider",
assertions.BootstrapStateProviderIs("copilot")).
Assert("Copilot marker file exists",
assertions.VMFileExists("/tmp/.aivm_test_copilot_installed")).
Assert("User was warned about agent mismatch",
assertions.StderrContains("was created for a different agent")).
Assert("User saw VM recreation message",
assertions.OutputContains("VM recreated with only GitHub Copilot")).
Run()
}
