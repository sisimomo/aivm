package e2e

// T3 Code integration tests.
//
// T3 Code is a background service: when enabled, `aivm start` launches t3 serve
// inside the VM and port-forwards it to the host as a daemon. The normal `aivm`
// agent terminal still launches as usual — T3 Code does not replace it. The idle
// monitor is disabled while T3 Code is running.

import (
	"testing"
	"time"

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
		Assert("Output contains T3 Code access URL", assertions.OutputContains("http://localhost:")).
		Assert("Output contains T3 Code pairing token", assertions.OutputContains("/pair#token=")).
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
		Assert("t3 binary installed in VM", assertions.VMRunOutput("t3 --version", "")).
		Run()
}

// TestT3CodeStopKillsTunnel verifies that `aivm stop` calls T3Code.Stop(),
// which removes the t3code-url state file.
func TestT3CodeStopKillsTunnel(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(0))

	h.Scenario("T3 Code mode: stop kills the SSH tunnel").
		Step("Start VM (T3 Code tunnel starts here)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("T3Code.Launch was called (tunnel started)", assertions.T3CodeLaunched()).
		Assert("Tunnel is running after start", assertions.StateFileExists("t3code-url")).
		Step("Run: aivm (bare) — agent terminal with tunnel already up", actions.CLI()).
		Step("Stop VM", actions.CLI("stop")).
		Assert("VM is stopped", assertions.VMStatus(vm.StatusStopped)).
		Assert("Tunnel is not running after stop", assertions.StateFileAbsent("t3code-url")).
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
		Assert("Tunnel is not running after stop", assertions.StateFileAbsent("t3code-url")).
		Step("Start VM again", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Tunnel restarted automatically", assertions.T3CodeLaunched()).
		Run()
}

// TestT3CodePortAccessible is the critical end-to-end validation: after
// `aivm start` the T3 Code port must be reachable via HTTP on localhost.
// This test uses no mocks for port verification — it makes a real TCP/HTTP
// connection to the port that Docker forwarded from the container running
// python3's built-in HTTP server (the test stub for `t3 serve`).
//
// A specific free port is used instead of 0 so that Docker maps the same
// port on the host as inside the container (host:N → container:N),
// allowing the t3 stub to bind on the correct port and be reachable.
func TestT3CodePortAccessible(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithT3Code(framework.FreePort()))

	h.Scenario("T3 Code: HTTP server is actually reachable on localhost after start").
		Step("Start VM with T3 Code enabled", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("T3Code.Launch was called", assertions.T3CodeLaunched()).
		Assert("T3 Code port is reachable via HTTP (no mocks)", assertions.T3CodePortAccessible()).
		Run()
}
