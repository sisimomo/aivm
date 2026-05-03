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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"aivm/internal/agent"
	"aivm/internal/cli"
	"aivm/internal/config"
	"aivm/internal/integration"
	"aivm/internal/lifecycle"
	"aivm/internal/monitor"
	"aivm/internal/plugin"
	"aivm/internal/providers/claude"
	"aivm/internal/providers/copilot"
	"aivm/internal/session"
	"aivm/internal/vm"
)

// Harness holds the full isolated test environment for one test.
// Each Harness gets a unique Docker container profile and temp state directory.
// Both are always cleaned up when the test finishes, even on failure.
type Harness struct {
	t        *testing.T
	tc       testConfig
	StateDir string
	Profile  string
	App      *cli.App
	// ContainerVMs provides access to all DockerVM instances created during the
	// test, keyed by profile name. Use this to inspect secondary VMs (e.g. the
	// new VM profile bootstrapped during a soft rebuild).
	ContainerVMs *ContainerVMRegistry
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
		filepath.Join(stateDir, ".claude", "projects"),
		tc.DevRoot,
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("harness: create dir %s: %v", dir, err)
		}
	}

	cfg := buildTestConfig(profile, stateDir, tc)

	containerVMs := NewContainerVMRegistry()
	primaryVM := newDockerVM(cfg.VM.Profile, cfg.StateDir)
	containerVMs.Register(primaryVM)
	factory := containerVMs.Factory()

	app := buildTestApp(t, cfg, tc, primaryVM, factory)

	h := &Harness{
		t:            t,
		tc:           tc,
		StateDir:     stateDir,
		Profile:      profile,
		App:          app,
		ContainerVMs: containerVMs,
	}

	t.Cleanup(func() {
		containerVMs.DestroyAll()
		if err := os.RemoveAll(testRunDir); err != nil {
			t.Logf("harness cleanup: remove test run dir %q: %v", testRunDir, err)
		}
	})

	return h
}

// GetOrCreateSecondaryVM returns the DockerVM for the given profile, creating a
// new container if one does not already exist. Use this in tests that need to
// inspect or control a secondary VM (e.g. the "-next" profile in a soft rebuild).
func (h *Harness) GetOrCreateSecondaryVM(profile string) vm.VM {
	return h.ContainerVMs.GetOrCreate(profile, h.StateDir)
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

func buildTestApp(t *testing.T, cfg *config.Config, tc testConfig, vmInst vm.VM, factory vm.VMFactory) *cli.App {
	t.Helper()

	agentReg := agent.NewRegistry()
	agentReg.Register(newMockProvider(claude.New()))
	agentReg.Register(newMockProvider(copilot.New()))

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
	mon.VMFactory = factory
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
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("harness: load agent defaults: %v", err)
	}
	for name, def := range agentDefs {
		defs[name] = def.ToPluginDef()
	}
	for name := range defs {
		stub := plugin.PluginDef{
			Description: name + " (test stub)",
			Check:       "[ -f /tmp/.aivm_test_" + name + "_installed ]",
			Install:     "touch /tmp/.aivm_test_" + name + "_installed",
		}
		reg.Set(plugin.NewYAMLPlugin(name, stub))
	}

	var confirmer lifecycle.Confirmer
	if tc.Interactive {
		confirmer = lifecycle.NewScriptedConfirmer(tc.StdinAnswers...)
	} else {
		confirmer = &lifecycle.SilentConfirmer{}
	}

	svc := &lifecycle.LifecycleService{
		Config:       cfg,
		VM:           vmInst,
		MCP:          mcpStub,
		Sessions:     sessions,
		Monitor:      mon,
		Registry:     reg,
		Agents:       agentReg,
		Provider:     prov,
		AgentDefs:    agentDefs,
		VMFactory:    factory,
		// Integrations: replace production scripts with lightweight marker-file
		// stubs so tests don't run real tool setup inside the container.
		Integrations: buildTestIntegrations(tc),
		Confirmer:    confirmer,
		// DoLaunch uses GetWorkDir to resolve the working directory.
		// In tests, return DevRoot so the CWD-under-DevRoot check always passes
		// without needing os.Chdir (which is process-global and unsafe in tests).
		GetWorkDir: func() (string, error) { return cfg.VM.DevRoot, nil },
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

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random hex: %v", err))
	}
	return hex.EncodeToString(b)
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
			When:      d.When,
			Configure: "touch /tmp/.aivm_test_integ_" + key,
		})
	}
	stubs = append(stubs, tc.Integrations...)
	return stubs
}

