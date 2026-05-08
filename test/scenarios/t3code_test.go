package scenarios

// T3 Code integration tests.
//
// T3 Code is a background service: when enabled, `aivm start` launches t3 serve
// inside the VM and port-forwards it to the host as a daemon. The normal `aivm`
// agent terminal still launches as usual — T3 Code does not replace it. The idle
// monitor is disabled while T3 Code is running.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/t3code"
	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestT3CodeIdleMonitorDisabled verifies that when T3 Code is enabled, the
// idle monitor is never started. The lifecycle service logs "T3 Code mode —
// idle monitoring disabled" instead of calling Monitor.EnsureRunning().
func TestT3CodeIdleMonitorDisabled(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code mode: idle monitor not started on aivm start").
		Step("Start VM with T3 Code enabled", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap completed", assertions.BootstrapComplete()).
		Assert("Idle monitor disabled message shown", assertions.OutputContains("T3 Code mode — idle monitoring disabled")).
		Assert("Idle monitor PID file NOT written", assertions.StateFileAbsent("idle-monitor.pid")).
		Run()
}

// TestT3CodeStartsWithVM verifies that `aivm start` launches the T3 Code tunnel
// and displays the pairing info, and that `aivm` (bare) still opens the agent
// terminal session normally alongside the running tunnel.
func TestT3CodeStartsWithVM(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code: tunnel starts with VM, agent terminal still launches").
		Step("Start VM (T3 Code starts here)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("T3Code.Launch was called by start", assertions.T3CodeLaunched()).
		Assert("User saw T3 Code server ready", assertions.OutputContains("T3 Code server is ready.")).
		Assert("User saw pairing token", assertions.OutputContains("Token:")).
		Assert("User saw pairing URL with localhost (not 127.0.0.1)", assertions.OutputContains("Pairing URL: http://localhost:")).
		Assert("VM-internal address not exposed to user", assertions.OutputNotContains("127.0.0.1")).
		Step("Run: aivm (bare) — agent terminal", actions.CLI()).
		Assert("Agent provider was launched", assertions.AgentLaunched()).
		Run()
}

// TestT3CodeStatusShowsURL verifies that `aivm status` displays the T3 Code
// access URL so users can easily find it without digging through config.
func TestT3CodeStatusShowsURL(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code: status shows access URL").
		Step("Start VM (T3 Code starts here)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run status command", actions.CLI("status")).
		Assert("Output contains T3 Code label", assertions.OutputContains("T3 Code")).
		Assert("Output contains T3 Code access URL with token", assertions.OutputContains(fmt.Sprintf("http://localhost:%d/pair#token=", h.T3CodePort()))).
		Run()
}

// TestT3CodePluginAutoInjected verifies that the "t3code" plugin is
// automatically installed when T3 Code mode is enabled, without the user
// needing to list it in plugins.enabled.
func TestT3CodePluginAutoInjected(t *testing.T) {
	t.Parallel()
	// WithT3Code is the only option — user did NOT explicitly add "t3code" to
	// plugins.enabled. The framework mirrors CompositionEngine's injection.
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code mode: t3code plugin auto-injected and installed during bootstrap").
		Step("Start VM (bootstrap runs t3code plugin)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap completed", assertions.BootstrapComplete()).
		Assert("t3code plugin stub ran", assertions.VMFileExists("/tmp/.aivm_test_t3code_installed")).
		Run()
}

// TestT3CodeStopKillsTunnel verifies that `aivm stop` calls T3Code.Stop(),
// which marks the tunnel as not running in the NoopManager.
func TestT3CodeStopKillsTunnel(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code mode: stop kills the SSH tunnel").
		Step("Start VM (T3 Code tunnel starts here)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("T3Code.Launch was called (tunnel started)", assertions.T3CodeLaunched()).
		Assert("Tunnel is running after start", assertT3CodeRunning(true)).
		Step("Run: aivm (bare) — agent terminal with tunnel already up", actions.CLI()).
		Step("Stop VM", actions.CLI("stop")).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Assert("Tunnel is not running after stop", assertT3CodeRunning(false)).
		Run()
}

// TestT3CodeRestartAfterStop verifies that stopping the VM and starting it again
// automatically restarts the T3 Code daemon and tunnel without requiring the user
// to run any additional command.
func TestT3CodeRestartAfterStop(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code mode: stop then start restarts tunnel automatically").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Tunnel started on first start", assertions.T3CodeLaunched()).
		Step("Stop VM", actions.CLI("stop")).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Assert("Tunnel is not running after stop", assertT3CodeRunning(false)).
		Step("Start VM again", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Tunnel restarted automatically", assertT3CodeRunning(true)).
		Assert("T3Code.Launch called twice (once per start)", assertions.T3CodeLaunchCount(2)).
		Run()
}
func assertT3CodeRunning(want bool) framework.AssertFunc {
	return func(_ context.Context, h *framework.Harness) error {
		mgr := h.App.Lifecycle.T3Code
		if nm, ok := mgr.(*t3code.NoopManager); ok {
			got := nm.IsRunning()
			if got != want {
				return fmt.Errorf("T3Code.IsRunning(): got %v, want %v", got, want)
			}
			return nil
		}
		return fmt.Errorf("T3Code manager is not a NoopManager (cannot check IsRunning in test)")
	}
}
