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

// TestSessionEnvSSHExpandsHostVar verifies that vm.session_env values are resolved
// from the host process environment and exported in the VM login shell opened by
// aivm ssh. Session vars must not be written to the persistent vm.env file.
func TestSessionEnvSSHExpandsHostVar(t *testing.T) {
	t.Parallel()

	const hostVar = "AIVM_HOST_SESSION_VAR"
	if err := os.Setenv(hostVar, "from_host"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(hostVar) })

	h := framework.New(t, framework.WithSessionEnv(map[string]string{
		"AIVM_SESSION_VAR": "${" + hostVar + "}",
	}))

	h.Scenario("session_env forwarded into ssh login shell").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("SSH: echo session var", actions.CLIWithStdin("echo $AIVM_SESSION_VAR\nexit\n", "ssh")).
		Assert("Session var visible in ssh output", assertions.OutputContains("from_host")).
		Assert("Session var not in persistent env file", assertions.VMRunOutput(
			"! grep -q AIVM_SESSION_VAR /etc/profile.d/aivm-user-env.sh 2>/dev/null; echo not_persisted",
			"not_persisted",
		)).
		Run()
}

// TestSessionEnvSSHReResolvesOnEachSession verifies that changing a host env var
// between ssh invocations updates the value seen inside the VM (session_env is
// not cached in the VM image).
func TestSessionEnvSSHReResolvesOnEachSession(t *testing.T) {
	// Cannot use t.Parallel() — test modifies host env via os.Setenv.

	const hostVar = "AIVM_CHANGING_SESSION_HOST_VAR"
	if err := os.Setenv(hostVar, "session_v1"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(hostVar) })

	h := framework.New(t, framework.WithSessionEnv(map[string]string{
		"AIVM_DYNAMIC_SESSION_VAR": "${" + hostVar + "}",
	}))

	h.Scenario("session_env re-resolved on each ssh").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("SSH (v1)", actions.CLIWithStdin("echo $AIVM_DYNAMIC_SESSION_VAR\nexit\n", "ssh")).
		Assert("First session sees v1", assertions.OutputContains("session_v1")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Update host env to v2", actions.RunFunc(func() error {
			return os.Setenv(hostVar, "session_v2")
		})).
		Step("SSH (v2)", actions.CLIWithStdin("echo $AIVM_DYNAMIC_SESSION_VAR\nexit\n", "ssh")).
		Assert("Second session sees v2", assertions.OutputContains("session_v2")).
		Run()
}

// TestSessionEnvUnsetHostVarExpandsEmpty verifies that a missing host reference
// expands to an empty string inside the VM session.
func TestSessionEnvUnsetHostVarExpandsEmpty(t *testing.T) {
	// Cannot use t.Parallel() — test modifies host env via os.Unsetenv.

	const hostVar = "AIVM_UNSET_SESSION_HOST_VAR"
	prev, had := os.LookupEnv(hostVar)
	_ = os.Unsetenv(hostVar)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(hostVar, prev)
		} else {
			_ = os.Unsetenv(hostVar)
		}
	})

	h := framework.New(t, framework.WithSessionEnv(map[string]string{
		"AIVM_EMPTY_SESSION_VAR": "${" + hostVar + "}",
	}))

	h.Scenario("session_env missing host var expands to empty").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("SSH: echo markers around empty var",
			actions.CLIWithStdin("echo marker:${AIVM_EMPTY_SESSION_VAR}:marker\nexit\n", "ssh")).
		Assert("Empty expansion between markers", assertions.OutputContains("marker::marker")).
		Run()
}
