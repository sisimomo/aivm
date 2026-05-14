package e2e

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

// TestVMEnvOnFirstBoot consolidates three previously-separate single-boot tests:
//
//   - env vars are written to /etc/profile.d/aivm-user-env.sh (was TestVMEnvAppliedOnFirstBoot)
//   - multiple vars are all written (was TestVMEnvMultipleVars)
//   - vars are accessible in a login shell (was TestVMEnvAccessibleInLoginShell)
//
// All three required exactly one VM boot with env vars pre-set, so they share
// a single harness to avoid redundant bootstrap cycles.
func TestVMEnvOnFirstBoot(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_BOOT_VAR":  "boot_value",
		"AIVM_SHELL_VAR": "shell_value",
		"AIVM_FIRST":     "alpha",
		"AIVM_SECOND":    "beta",
		"AIVM_THIRD":     "gamma",
	}))

	h.Scenario("vm.env applied and accessible on first boot").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Bootstrap state records env_hash", assertions.BootstrapEnvHashSet()).
		Assert("Env file exists in VM", assertions.VMFileExists("/etc/profile.d/aivm-user-env.sh")).
		Assert("Env file contains AIVM_BOOT_VAR name", assertions.VMRunOutput(
			"grep AIVM_BOOT_VAR /etc/profile.d/aivm-user-env.sh",
			"AIVM_BOOT_VAR",
		)).
		Assert("Env file contains AIVM_BOOT_VAR value", assertions.VMRunOutput(
			"grep AIVM_BOOT_VAR /etc/profile.d/aivm-user-env.sh",
			"boot_value",
		)).
		Assert("Env var accessible after sourcing profile.d", assertions.VMRunOutput(
			". /etc/profile.d/aivm-user-env.sh && echo $AIVM_SHELL_VAR",
			"shell_value",
		)).
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
// Also covers the first-boot expansion check previously in TestVMEnvHostVarExpansion:
// the first half of this scenario already confirms that ${HOST_VAR} is expanded
// on initial bootstrap, so a separate test is not needed.
//
//  1. Set host var to v1, first start — env file contains resolved "value_v1".
//  2. Update host var to v2.
//  3. Second start: env hash differs → envChangedStep fires, file updated to v2.
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
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Update host env var to v2", actions.RunFunc(func() error {
			return os.Setenv("AIVM_CHANGING_HOST_VAR", "value_v2")
		})).
		Step("Start VM again — host var changed, env hash differs", actions.CLI("start")).
		Assert("VM still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Env file now contains v2", assertions.VMRunOutput(
			"grep AIVM_DYNAMIC_VAR /etc/profile.d/aivm-user-env.sh",
			"value_v2",
		)).
		Run()
}

// TestVMEnvNoChangeIsIdempotent verifies that a second start with an unchanged
// vm.env does NOT trigger envChangedStep.
//
//  1. First start: full bootstrap with vm.env set.
//  2. Second start (same config): no change detected — "VM is up to date" shown.
func TestVMEnvNoChangeIsIdempotent(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithVMEnv(map[string]string{
		"AIVM_IDEMPOTENT_VAR": "same_value",
	}))

	h.Scenario("vm.env unchanged — no re-apply on second start").
		Step("Start VM (first boot)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Start VM again — env unchanged", actions.CLI("start")).
		Assert("VM still running", assertions.VMStatus(vm.StatusRunning)).
		Assert("User saw up-to-date message", assertions.OutputContains("VM is up to date")).
		Run()
}

// TestVMEnvClearedWhenRemoved verifies that removing all env vars after they
// were applied causes envChangedStep to overwrite the file with an empty shell
// file (no export statements remain).
//
//  1. First start: vm.env set — env file written.
//  2. Clear vm.env to empty map.
//  3. Second start: envChangedStep detects change, overwrites env file.
//  4. Env file exists but no longer exports the old variable.
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
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Clear vm.env (remove all vars)", actions.ChangeVMEnv(map[string]string{})).
		Step("Start VM again — env cleared", actions.CLI("start")).
		Assert("VM still running (not recreated)", assertions.VMStatus(vm.StatusRunning)).
		Assert("Env file no longer exports old variable", assertions.VMRunOutput(
			"grep -c AIVM_REMOVE_ME /etc/profile.d/aivm-user-env.sh || true",
			"0",
		)).
		Run()
}
