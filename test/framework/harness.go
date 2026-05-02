// Package framework provides the integration testing harness for AIVM.
// It creates isolated mock VM environments per test, wires up the full
// cli.App (with a no-op MCP stub), and tears everything down on completion.
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
	"testing"

	"aivm/internal/agent"
	"aivm/internal/cli"
	"aivm/internal/config"
	"aivm/internal/monitor"
	"aivm/internal/plugin"
	"aivm/internal/providers/claude"
	"aivm/internal/providers/copilot"
	"aivm/internal/session"
	"aivm/internal/vm"
)

// Harness holds the full isolated test environment for one test.
// Each Harness gets a unique mock VM profile and temp state directory.
// The state dir is always removed when the test finishes (even on failure).
type Harness struct {
	t        *testing.T
	tc       testConfig
	StateDir string
	Profile  string
	App      *cli.App
	// MockVMs provides access to all MockVM instances created during the test,
	// keyed by profile name. Use this to assert on secondary VM state (e.g.
	// the legacy VM destroyed during a soft rebuild).
	MockVMs *MockVMRegistry
}

// New creates a new Harness for the calling test.
// The Harness is fully wired (cli.App with mock VM and stub MCP) and registers
// a t.Cleanup that removes the temp state directory.
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()

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

	mockVMs := NewMockVMRegistry()
	primaryVM := newMockVM(cfg.VM.Profile, cfg.StateDir)
	mockVMs.Register(primaryVM)
	factory := mockVMs.Factory()

	app := buildTestApp(t, cfg, tc, primaryVM, factory)

	h := &Harness{
		t:        t,
		tc:       tc,
		StateDir: stateDir,
		Profile:  profile,
		App:      app,
		MockVMs:  mockVMs,
	}

	t.Cleanup(func() {
		if err := os.RemoveAll(testRunDir); err != nil {
			t.Logf("harness cleanup: remove test run dir %q: %v", testRunDir, err)
		}
	})

	return h
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
		if err := h.App.Monitor.Run(monCtx); err != nil && err != context.Canceled {
			h.t.Logf("monitor exited: %v", err)
		}
	}()
	return cancel
}

// ImageManager returns the ImageManager for the test VM, scoped to StateDir.
func (h *Harness) ImageManager() *vm.ImageManager {
	return vm.NewImageManager(h.App.VM, h.StateDir)
}

func buildTestApp(t *testing.T, cfg *config.Config, tc testConfig, vmInst vm.VM, factory vm.VMFactory) *cli.App {
	t.Helper()

	agentReg := agent.NewRegistry()
	agentReg.Register(claude.New())
	agentReg.Register(copilot.New())

	prov, ok := agentReg.Get("claude")
	if !ok {
		t.Fatal("harness: claude provider not registered")
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

	reg := plugin.Global()
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load plugin defaults: %v", err)
	}
	for name, def := range defs {
		reg.Set(plugin.NewYAMLPlugin(name, def))
	}

	return &cli.App{
		Config:    cfg,
		VM:        vmInst,
		MCP:       mcpStub,
		Sessions:  sessions,
		Monitor:   mon,
		Registry:  reg,
		Agents:    agentReg,
		Provider:  prov,
		VMFactory: factory,
	}
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random hex: %v", err))
	}
	return hex.EncodeToString(b)
}

