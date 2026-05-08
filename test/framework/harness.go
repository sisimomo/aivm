// Package framework provides the integration testing harness for AIVM.
// It creates isolated Docker-container VM environments per test, wires up the
// full cli.App (with a no-op MCP stub), and tears everything down on completion.
//
// Each test gets a dedicated Ubuntu container that behaves like a real Linux VM.
// Bootstrap scripts execute inside the container via docker exec, so toolchain
// installation, plugin check scripts, and configure steps all run for real.
//
// Usage:
//
//	func TestMyScenario(t *testing.T) {
//	    h := framework.New(t, framework.WithCPUs(2))
//	    h.Scenario("my scenario").
//	        Step("Start VM", actions.Start()).
//	        Wait("VM running", conditions.VMStatus(vm.StatusRunning), 90*time.Second).
//	        Assert("Bootstrap complete", assertions.BootstrapComplete()).
//	        Run()
//	}
package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/cli"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/lifecycle"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/monitor"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/providers/generic"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/t3code"
	"github.com/sisimomo/aivm/internal/vm"
)

// Harness holds the full isolated test environment for one test.
// Each Harness gets a unique Docker container profile and temp state directory.
// Both are always cleaned up when the test finishes, even on failure.
type Harness struct {
	t           *testing.T
	tc          testConfig
	StateDir    string
	Profile     string
	App         *cli.App
	t3codePort  int
	// Output captures all stdout/stderr written by the LifecycleService logger.
	// Use Output.Stdout() / Output.Stderr() in assertions, and Output.Reset()
	// between RunCLI calls when per-command isolation matters.
	Output *OutputBuffer
}

// New creates a new Harness for the calling test.
// The Harness is fully wired (cli.App with a Docker container VM and stub MCP)
// and registers a t.Cleanup that stops all containers and removes the temp state
// directory.
//
// Requires the aivm-test-base:latest Docker image to be present. Build it once
// with: make build-test-image
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()

	// Ensure the base Docker image exists, building it if needed.
	if err := EnsureTestImage(testDockerDir()); err != nil {
		t.Fatalf("harness: ensure test image: %v", err)
	}

	tc := defaultTestConfig()
	for _, opt := range opts {
		opt(&tc)
	}

	suffix := mustRandomHex(6)
	profile := "aivm-test-" + suffix

	// Use ~/.aivm/test-runs/ so the path is stable and easy to find.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("harness: get home dir: %v", err)
	}
	testRunDir := filepath.Join(home, ".aivm", "test-runs", profile)
	stateDir := filepath.Join(testRunDir, "state")

	if tc.DevRoot == "" {
		tc.DevRoot = filepath.Join(testRunDir, "dev")
	}

	for _, dir := range []string{
		stateDir,
		filepath.Join(stateDir, "logs"),
		filepath.Join(stateDir, "sessions"),
		tc.DevRoot,
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("harness: create dir %s: %v", dir, err)
		}
	}

	// Create agent-specific and T3 Code persistence directories.
	// Driven by the agent's Persist field — no code change needed for new agents.
	allAgentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("harness: load agent defs: %v", err)
	}
	if def, ok := allAgentDefs[tc.Provider]; ok {
		for _, rel := range def.Persist {
			if err := os.MkdirAll(filepath.Join(stateDir, rel), 0755); err != nil {
				t.Fatalf("harness: create persist dir %s: %v", rel, err)
			}
		}
	}
	if tc.T3CodeEnabled {
		if err := os.MkdirAll(filepath.Join(stateDir, ".t3"), 0755); err != nil {
			t.Fatalf("harness: create t3code dir: %v", err)
		}
	}

	// Auto-assign a free port for T3 Code tests to prevent parallel tests from
	// competing for the same port when docker-binding it at container creation.
	if tc.T3CodeEnabled && tc.T3CodePort == 0 {
		port, err := findFreePort()
		if err != nil {
			t.Fatalf("harness: find free port for T3Code: %v", err)
		}
		tc.T3CodePort = port
	}

	cfg := buildTestConfig(profile, stateDir, tc)

	containerVMs := NewContainerVMRegistry()
	primaryVM := vm.NewDocker(cfg.VM.Profile(), cfg.StateDir, testImageName)
	containerVMs.Register(primaryVM)
	trackingVM := NewRunTrackingVM(primaryVM)

	output := &OutputBuffer{}
	app := buildTestApp(t, cfg, tc, trackingVM, output)

	h := &Harness{
		t:          t,
		tc:         tc,
		StateDir:   stateDir,
		Profile:    profile,
		App:        app,
		t3codePort: tc.T3CodePort,
		Output:     output,
	}

	t.Cleanup(func() {
		containerVMs.DestroyAll()
		if err := os.RemoveAll(testRunDir); err != nil {
			t.Logf("harness cleanup: remove test run dir %q: %v", testRunDir, err)
		}
	})

	return h
}

// RunCLI executes an aivm CLI command through the real Cobra entry point,
// using the harness's pre-built mock App. This is the preferred way to invoke
// commands in tests — it exercises the same code path as a real user invocation,
// including flag parsing and cobra routing.
//
// Example:
//
//	h.RunCLI(ctx, "start")
//	h.RunCLI(ctx, "bootstrap", "--force")
//	h.RunCLI(ctx, "bootstrap", "--plugin", "java")
func (h *Harness) RunCLI(ctx context.Context, args ...string) error {
	root := cli.NewRootCmd("test", func(_ string) (*cli.App, error) {
		return h.App, nil
	})
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

// Scenario creates a new Scenario builder attached to this Harness.
func (h *Harness) Scenario(name string) *Scenario {
	return newScenario(name, h)
}

// RunMonitorInProcess starts the idle monitor as an in-process goroutine
// (instead of the fork-exec daemon used in production). This is required
// for testing idle-based lifecycle transitions.
//
// Returns a cancel function that stops the monitor. The monitor is also
// stopped automatically when the parent test context is done.
func (h *Harness) RunMonitorInProcess(ctx context.Context) context.CancelFunc {
	monCtx, cancel := context.WithCancel(ctx)
	go func() {
		if err := h.App.Lifecycle.Monitor.Run(monCtx); err != nil && err != context.Canceled {
			h.t.Logf("monitor exited: %v", err)
		}
	}()
	return cancel
}

// ProviderLaunchCount returns the number of times the active agent provider's
// Launch method was called. All harness-created Apps use MockProvider, so this
// is always accurate. Use with assertions.AgentLaunched() or assertions.AgentLaunchCount().
func (h *Harness) ProviderLaunchCount() int {
	if mp, ok := h.App.Lifecycle.Provider.(*MockProvider); ok {
		return mp.LaunchCallCount()
	}
	return 0
}

// ImageManager returns the ImageManager for the test VM, scoped to StateDir.
func (h *Harness) ImageManager() *vm.ImageManager {
	return vm.NewImageManager(h.App.Lifecycle.VM, h.StateDir)
}

// T3CodeLaunchCount returns the number of times T3Code.Launch was called.
// Only accurate when the harness was built with WithT3Code (otherwise returns 0).
func (h *Harness) T3CodeLaunchCount() int {
	if nm, ok := h.App.Lifecycle.T3Code.(*t3code.NoopManager); ok {
		return nm.LaunchCallCount()
	}
	return 0
}

func buildTestApp(t *testing.T, cfg *config.Config, tc testConfig, vmInst vm.VM, output *OutputBuffer) *cli.App {
	t.Helper()

	agentReg := agent.NewRegistry()
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("harness: load agent defs: %v", err)
	}
	for name, def := range agentDefs {
		agentReg.Register(newMockProvider(generic.NewFromDef(name, def)))
	}

	prov, ok := agentReg.Get(tc.Provider)
	if !ok {
		t.Fatalf("harness: provider %q not registered", tc.Provider)
	}

	sessions := session.NewStore(cfg.StateDir)
	mcpStub := &NoopMCP{}
	mon := monitor.NewIdleMonitor(
		sessions, vmInst, mcpStub,
		tc.IdleTimeout, tc.DeleteTimeout, cfg.StateDir,
	)
	mon.PollInterval = tc.PollInterval
	mon.DisableDaemonLaunch = true

	// Create a per-test plugin registry so parallel tests don't share global state.
	// Load plugin names from defaults, then register trivial test stubs for all of
	// them. Stubs use marker files in /tmp so the full bootstrap state machine is
	// exercised without any apt-get or network operations.
	reg := plugin.NewRegistry()
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load plugin defaults: %v", err)
	}
	for name, def := range agentDefs {
		defs[name] = def.ToPluginDef()
	}
	stubDefs := make(map[string]plugin.PluginDef, len(defs))
	for name := range defs {
		stub := plugin.PluginDef{
			Description: name + " (test stub)",
			SkipIf:      "[ -f /tmp/.aivm_test_" + name + "_installed ]",
			Setup:       "touch /tmp/.aivm_test_" + name + "_installed",
		}
		// t3code stub also installs a mock `t3` binary so that launchT3Code()
		// can start a background server and read its pairing token output.
		if name == "t3code" {
			stub.Setup = `touch /tmp/.aivm_test_t3code_installed
sudo tee /usr/local/bin/t3 > /dev/null << 'EOFT3'
#!/bin/bash
if [ "$1" = "serve" ]; then
    PORT=3773
    while [ $# -gt 0 ]; do
        if [ "$1" = "--port" ]; then PORT="$2"; fi
        shift
    done
    echo "T3 Code server is ready."
    echo "Connection string: http://127.0.0.1:$PORT"
    echo "Token: test-pairing-token-stub"
    echo "Pairing URL: http://127.0.0.1:$PORT/pair#token=test-pairing-token-stub"
    # Stay alive so the background nohup process keeps running.
    while true; do sleep 60; done
fi
EOFT3
sudo chmod +x /usr/local/bin/t3`
		}
		stubDefs[name] = stub
		reg.Set(plugin.NewYAMLPlugin(name, stub))
	}

	var confirmer lifecycle.Confirmer
	if tc.Interactive {
		confirmer = lifecycle.NewScriptedConfirmer(tc.StdinAnswers...)
	} else {
		confirmer = &lifecycle.SilentConfirmer{}
	}

	t3codeMgr := &t3code.NoopManager{}

	svc := &lifecycle.LifecycleService{
		Config:     cfg,
		VM:         vmInst,
		MCP:        mcpStub,
		T3Code:     t3codeMgr,
		Sessions:   sessions,
		Monitor:    mon,
		Registry:   reg,
		Agents:     agentReg,
		Provider:   prov,
		AgentDefs:  agentDefs,
		PluginDefs: stubDefs,
		// Integrations: replace production scripts with lightweight marker-file
		// stubs so tests don't run real tool setup inside the container.
		Integrations: buildTestIntegrations(tc),
		Confirmer:    confirmer,
		// DoLaunch uses GetWorkDir to resolve the working directory.
		// In tests, return DevRoot so the CWD-under-mount check always passes
		// without needing os.Chdir (which is process-global and unsafe in tests).
		GetWorkDir: func() (string, error) { return tc.DevRoot, nil },
		// Log routes all user-visible output through the per-test OutputBuffer.
		Log: aivmlog.New(output, &stderrWriter{output}),
	}

	return &cli.App{
		Lifecycle: svc,
	}
}

// testDockerDir returns the absolute path to the test/docker/ directory
// containing the Dockerfile for the container base image.
func testDockerDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "docker")
}

// T3CodePort returns the port assigned to T3 Code for this harness.
// Valid only when the harness was created with WithT3Code.
func (h *Harness) T3CodePort() int {
	return h.t3codePort
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random hex: %v", err))
	}
	return hex.EncodeToString(b)
}

// findFreePort asks the OS for an available TCP port by binding to :0.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// buildTestIntegrations replaces production integration scripts with stub
// marker-file commands so tests don't invoke real toolchain setup. For each
// built-in integration a stub is created that writes a marker file to /tmp
// named after the integration's key (colons replaced with underscores). Any
// tc.Integrations are appended verbatim (caller-supplied).
func buildTestIntegrations(tc testConfig) []integration.IntegrationDef {
	defaults, err := integration.LoadDefaults()
	if err != nil {
		panic(fmt.Sprintf("harness: load integration defaults: %v", err))
	}
	stubs := make([]integration.IntegrationDef, 0, len(defaults)+len(tc.Integrations))
	for _, d := range defaults {
		key := strings.ReplaceAll(d.Key(), ":", "_")
		stubs = append(stubs, integration.IntegrationDef{
			Name:      d.Name,
			From:      d.From,
			To:        d.To,
			SkipIf:    "[ -f /tmp/.aivm_test_integ_" + key + " ]",
			Configure: "touch /tmp/.aivm_test_integ_" + key,
		})
	}
	stubs = append(stubs, tc.Integrations...)
	return stubs
}
