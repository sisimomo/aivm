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

// TestVMMaxAgeRecreationAccepted verifies that when the VM is older than the
// configured MaxAgeDays threshold and the user answers "y", DoStart destroys
// the VM and creates a fresh one.
//
//  1. Start VM — bootstrap runs, base image saved, vm-created-at written.
//  2. Stop the VM.
//  3. Backdate vm-created-at by 31 days (threshold is 30).
//  4. Start VM again in interactive mode, answer "y":
//     "VM is 31 day(s) old — Delete and recreate? [y/N]" → y
//  5. VM is destroyed and a new one is created; bootstrap runs again.
func TestVMMaxAgeRecreationAccepted(t *testing.T) {
	h := framework.New(t,
		framework.WithMaxAgeDays(30),
		framework.WithInteractive("y"), // "Delete and recreate for a clean slate? [y/N]"
	)

	h.Scenario("VM too old — user accepts recreation").
		Step("Start VM (first boot, vm-created-at written)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image saved after first boot", assertions.BaseImageExists()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Step("Backdate vm-created-at by 31 days (exceeds 30-day threshold)",
			actions.SetVMCreatedDaysAgo(31)).
		Step("Start VM — age check triggers, user accepts recreation", actions.CLI("start")).
		Wait("VM is running after recreation", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap ran on new VM", assertions.VMRunCountAtLeast(1)).
		Assert("Bootstrap state valid after recreation", assertions.BootstrapComplete()).
		Run()
}

// TestVMMaxAgeRecreationDeclined verifies that when the VM is older than
// MaxAgeDays and the user answers "n", DoStart simply resumes the existing VM
// without destroying it.
//
//  1. Start VM — vm-created-at written.
//  2. Stop the VM.
//  3. Backdate vm-created-at by 31 days.
//  4. Start VM in interactive mode, answer "n":
//     "VM is 31 day(s) old — Delete and recreate? [y/N]" → n
//  5. VM is resumed (status Running); bootstrap state is unchanged.
func TestVMMaxAgeRecreationDeclined(t *testing.T) {
	h := framework.New(t,
		framework.WithMaxAgeDays(30),
		framework.WithInteractive("n"), // "Delete and recreate for a clean slate? [y/N]"
	)

	h.Scenario("VM too old — user declines recreation, VM resumed as-is").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Step("Backdate vm-created-at by 31 days", actions.SetVMCreatedDaysAgo(31)).
		Step("Reset run counter", actions.ResetMockVMRunCount()).
		Step("Start VM — user declines recreation", actions.CLI("start")).
		Wait("VM is running (resumed, not recreated)", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("No new bootstrap ran (VM was not recreated)", assertions.VMRunCountIs(0)).
		Assert("Bootstrap state unchanged", assertions.BootstrapComplete()).
		Run()
}

// TestVMMaxAgeNonInteractiveSkipsPrompt verifies that when MaxAgeDays is
// exceeded in a non-interactive context (e.g. CI), the age check is skipped
// silently and the VM is resumed without any prompt.
//
//  1. Start VM — vm-created-at written.
//  2. Stop the VM.
//  3. Backdate vm-created-at by 31 days.
//  4. Start VM in non-interactive mode (default) — no prompt, VM resumed.
func TestVMMaxAgeNonInteractiveSkipsPrompt(t *testing.T) {
	h := framework.New(t,
		framework.WithMaxAgeDays(30),
		// no WithInteractive — simulates CI / automated run
	)

	h.Scenario("VM too old but non-interactive — age prompt skipped, VM resumed").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Step("Backdate vm-created-at by 31 days", actions.SetVMCreatedDaysAgo(31)).
		Step("Start VM — non-interactive, age prompt silently skipped", actions.CLI("start")).
		Wait("VM is running (resumed without prompt)", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("VM is running after silent resume", assertions.VMStatus(vm.StatusRunning)).
		Run()
}
