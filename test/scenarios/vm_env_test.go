package scenarios

import (
	"os"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestVMEnvAppliedOnFirstBoot verifies that env vars configured in vm.env are
// written to /etc/profile.d/aivm-user-env.sh during the initial full bootstrap.
//
//  1. Create harness with vm.env pre-configured.
//  2. Start VM (first boot — full bootstrap runs).
//  3. Env file is present in the VM and contains the configured variable.
//  4. Bootstrap state records a non-empty env_hash.
func TestVMEnvAppliedOnFirstBoot(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_BOOT_VAR": "boot_value",
	}))

	h.Scenario("vm.env applied during initial bootstrap").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Bootstrap state records env_hash", assertions.BootstrapEnvHashSet()).
		Assert("Env file exists in VM", assertions.VMFileExists("/etc/profile.d/aivm-user-env.sh")).
		Assert("Env file contains the variable name", assertions.VMRunOutput(
			"grep AIVM_BOOT_VAR /etc/profile.d/aivm-user-env.sh",
			"AIVM_BOOT_VAR",
		)).
		Assert("Env file contains the variable value", assertions.VMRunOutput(
			"grep AIVM_BOOT_VAR /etc/profile.d/aivm-user-env.sh",
			"boot_value",
		)).
		Run()
}

// TestVMEnvAccessibleInLoginShell verifies that env vars applied via vm.env are
// actually accessible in a login shell session inside the VM (i.e. /etc/profile.d
// is sourced).
//
//  1. Create harness with vm.env pre-configured.
//  2. Start VM (first boot).
//  3. Source the env file explicitly and echo the variable — value is visible.
func TestVMEnvAccessibleInLoginShell(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_SHELL_VAR": "shell_value",
	}))

	h.Scenario("vm.env accessible in login shell").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Env var accessible after sourcing profile.d", assertions.VMRunOutput(
			". /etc/profile.d/aivm-user-env.sh && echo $AIVM_SHELL_VAR",
			"shell_value",
		)).
		Run()
}

// TestVMEnvHostVarExpansion verifies that ${HOST_VAR} placeholders in vm.env
// values are expanded from the host environment at sync time.
//
//  1. Set a host env var.
//  2. Create harness with vm.env referencing it via ${...}.
//  3. Start VM (first boot).
//  4. Env file contains the *resolved* value, not the placeholder.
func TestVMEnvHostVarExpansion(t *testing.T) {
	// Cannot use t.Parallel() — test modifies host env via t.Setenv.
	t.Setenv("AIVM_TEST_HOST_VALUE", "expanded_from_host")

	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"MY_EXPANDED_VAR": "${AIVM_TEST_HOST_VALUE}",
	}))

	h.Scenario("vm.env ${HOST_VAR} expansion").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Env file contains expanded value", assertions.VMRunOutput(
			"grep MY_EXPANDED_VAR /etc/profile.d/aivm-user-env.sh",
			"expanded_from_host",
		)).
		Assert("Env file does NOT contain raw placeholder", assertions.VMRunOutput(
			"grep -v AIVM_TEST_HOST_VALUE /etc/profile.d/aivm-user-env.sh || true",
			"MY_EXPANDED_VAR",
		)).
		Run()
}

// TestVMEnvNoChangeIsIdempotent verifies that a second start with an unchanged
// vm.env does NOT trigger envChangedStep — no extra VM.Run() calls.
//
//  1. First start: full bootstrap with vm.env set.
//  2. Reset run counter.
//  3. Second start (same config): no change detected — 0 VM.Run() calls.
func TestVMEnvNoChangeIsIdempotent(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_IDEMPOTENT_VAR": "same_value",
	}))

	h.Scenario("vm.env unchanged — no re-apply on second start").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM again — env unchanged", actions.CLI("start")).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("No VM scripts ran (up-to-date step)", assertions.VMRunCountIs(0)).
		Assert("User saw up-to-date message", assertions.OutputContains("VM is up to date")).
		Run()
}

// TestVMEnvClearedWhenRemoved verifies that removing all env vars after they
// were applied causes envChangedStep to overwrite the file with an empty shell
// file (no export statements remain).
//
//  1. First start: vm.env set — env file written.
//  2. Reset run counter.
//  3. Clear vm.env to empty map.
//  4. Second start: envChangedStep detects change, overwrites env file.
//  5. Env file exists but no longer exports the old variable.
func TestVMEnvClearedWhenRemoved(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_REMOVE_ME": "gone",
	}))

	h.Scenario("vm.env cleared — env file emptied in-place").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Env file initially contains variable", assertions.VMRunOutput(
			"grep AIVM_REMOVE_ME /etc/profile.d/aivm-user-env.sh",
			"AIVM_REMOVE_ME",
		)).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Clear vm.env (remove all vars)", actions.ChangeVMEnv(map[string]string{})).
		Step("Start VM again — env cleared", actions.CLI("start")).
		Assert("VM still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Exactly one VM Run call (env file write only)", assertions.VMRunCountIs(1)).
		Assert("Env file no longer exports old variable", assertions.VMRunOutput(
			"grep -c AIVM_REMOVE_ME /etc/profile.d/aivm-user-env.sh || true",
			"0",
		)).
		Run()
}

// TestVMEnvMultipleVars verifies that multiple env vars are all written to the
// env file and accessible in the VM.
func TestVMEnvMultipleVars(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_FIRST":  "alpha",
		"AIVM_SECOND": "beta",
		"AIVM_THIRD":  "gamma",
	}))

	h.Scenario("vm.env multiple variables applied on first boot").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Env file contains AIVM_FIRST", assertions.VMRunOutput(
			"grep AIVM_FIRST /etc/profile.d/aivm-user-env.sh",
			"alpha",
		)).
		Assert("Env file contains AIVM_SECOND", assertions.VMRunOutput(
			"grep AIVM_SECOND /etc/profile.d/aivm-user-env.sh",
			"beta",
		)).
		Assert("Env file contains AIVM_THIRD", assertions.VMRunOutput(
			"grep AIVM_THIRD /etc/profile.d/aivm-user-env.sh",
			"gamma",
		)).
		Run()
}

// TestVMEnvHostVarExpansionReappliedOnHostChange verifies that if a host env var
// used in vm.env changes between starts, envChangedStep detects the new hash
// (because the resolved value changed) and re-applies the env file.
//
//  1. Set host var to v1, first start.
//  2. Reset run counter.
//  3. Update host var to v2 (simulated via os.Setenv before second start).
//  4. Second start: env hash differs (resolved value changed) → envChangedStep fires.
//  5. Env file now contains v2.
func TestVMEnvHostVarExpansionReappliedOnHostChange(t *testing.T) {
	// Cannot use t.Parallel() — test modifies host env via os.Setenv.

	if err := os.Setenv("AIVM_CHANGING_HOST_VAR", "value_v1"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { os.Unsetenv("AIVM_CHANGING_HOST_VAR") })

	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_DYNAMIC_VAR": "${AIVM_CHANGING_HOST_VAR}",
	}))

	h.Scenario("vm.env host-var change triggers re-apply").
		Step("Start VM (first boot — v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Env file contains v1", assertions.VMRunOutput(
			"grep AIVM_DYNAMIC_VAR /etc/profile.d/aivm-user-env.sh",
			"value_v1",
		)).
		Step("Reset VM run counter", actions.ResetMockVMRunCount()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Update host env var to v2", actions.RunFunc(func() error {
			return os.Setenv("AIVM_CHANGING_HOST_VAR", "value_v2")
		})).
		Step("Start VM again — host var changed, env hash differs", actions.CLI("start")).
		Assert("VM still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Exactly one VM Run call (env file write only)", assertions.VMRunCountIs(1)).
		Assert("Env file now contains v2", assertions.VMRunOutput(
			"grep AIVM_DYNAMIC_VAR /etc/profile.d/aivm-user-env.sh",
			"value_v2",
		)).
		Run()
}
