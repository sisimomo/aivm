//go:build plugin_install

package plugininstall

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
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
)

// PluginHarness is a lightweight test helper that runs real plugin install scripts
// inside a fresh Docker container. It uses the real plugin.Executor (no stubs) so
// template rendering, dependency ordering, and bootstrap logic are all exercised.
type PluginHarness struct {
	t        *testing.T
	vm       *framework.DockerVM
	reg      *plugin.Registry
	defs     map[string]plugin.PluginDef
	stateDir string
}

// newPluginHarness builds a fresh Docker container and loads all real plugin and
// agent definitions into a plugin.Registry. The container and temp state dir are
// removed automatically when the test ends.
func newPluginHarness(t *testing.T) *PluginHarness {
	t.Helper()

	if err := framework.EnsureTestImage(testDockerDir()); err != nil {
		t.Fatalf("harness: ensure test image: %v", err)
	}

	suffix := mustRandomHex(6)
	profile := "aivm-pitest-" + suffix

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("harness: get home dir: %v", err)
	}
	testRunDir := filepath.Join(home, ".aivm", "test-runs", profile)
	stateDir := filepath.Join(testRunDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("harness: create state dir: %v", err)
	}

	dockerVM := framework.NewDockerVM(profile, stateDir)

	ctx := context.Background()
	if err := dockerVM.Start(ctx, vm.StartOptions{}); err != nil {
		t.Fatalf("harness: start container: %v", err)
	}
	if err := dockerVM.WaitReady(ctx, 30*time.Second); err != nil {
		t.Fatalf("harness: wait container ready: %v", err)
	}

	// Load real plugin definitions.
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load plugin defaults: %v", err)
	}

	// Merge agent definitions as plugins (claude, copilot).
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("harness: load agent defaults: %v", err)
	}
	for name, def := range agentDefs {
		defs[name] = def.ToPluginDef()
	}

	reg := plugin.NewRegistry()
	for name, def := range defs {
		reg.Set(plugin.NewYAMLPlugin(name, def))
	}

	h := &PluginHarness{
		t:        t,
		vm:       dockerVM,
		reg:      reg,
		defs:     defs,
		stateDir: stateDir,
	}

	t.Cleanup(func() {
		_ = dockerVM.Destroy(context.Background())
		os.RemoveAll(testRunDir) //nolint:errcheck
	})

	return h
}

// Install runs the named plugin (and all its transitive dependencies) inside the
// container using the real bootstrap engine. cfg overrides per-plugin config for
// the plugin under test; dependencies use their own defaults.
func (h *PluginHarness) Install(pluginName string, cfg map[string]any) {
	h.t.Helper()
	pluginConfigs := map[string]map[string]any{}
	if len(cfg) > 0 {
		pluginConfigs[pluginName] = cfg
	}

	exec := &plugin.Executor{
		Registry:     h.reg,
		Enabled:      []string{pluginName},
		PluginConfig: pluginConfigs,
		StateDir:     h.stateDir,
		VMInst:       h.vm,
	}
	if err := exec.Run(context.Background(), true); err != nil {
		h.t.Fatalf("Install %q: %v", pluginName, err)
	}
}

// AssertCommand runs cmd inside the container as a login shell and asserts that
// the combined output contains wantSubstr. Fails the test on error or mismatch.
func (h *PluginHarness) AssertCommand(cmd, wantSubstr string) {
	h.t.Helper()
	output, err := h.vm.RunOutput(context.Background(), cmd, nil)
	if err != nil {
		h.t.Fatalf("AssertCommand %q: command failed: %v\noutput:\n%s", cmd, err, output)
	}
	if wantSubstr != "" && !strings.Contains(output, wantSubstr) {
		h.t.Fatalf("AssertCommand %q: output does not contain %q\nfull output:\n%s", cmd, wantSubstr, output)
	}
}

// AssertSkipIf runs the named plugin's skip_if script inside the container and
// asserts that it exits 0 (meaning "already installed — skip"). This validates
// idempotency: after a successful install the plugin must detect itself as set up.
//
// cfg may be nil; in that case the plugin's own defaults are used (same as Install).
func (h *PluginHarness) AssertSkipIf(pluginName string, cfg map[string]any) {
	h.t.Helper()

	p, ok := h.reg.Get(pluginName)
	if !ok {
		h.t.Fatalf("AssertSkipIf: plugin %q not found in registry", pluginName)
	}

	// Build effective config: plugin defaults merged with caller overrides.
	def := h.defs[pluginName]
	effective := make(map[string]any, len(def.Defaults)+len(cfg))
	for k, v := range def.Defaults {
		effective[k] = v
	}
	for k, v := range cfg {
		effective[k] = v
	}

	env := plugin.InstallEnv{
		Config:   effective,
		StateDir: h.stateDir,
		VM:       h.vm,
	}

	skip, err := p.SkipIf(context.Background(), env)
	if err != nil {
		h.t.Fatalf("AssertSkipIf %q: skip_if script error: %v", pluginName, err)
	}
	if !skip {
		h.t.Fatalf("AssertSkipIf %q: skip_if returned false — plugin should be detected as installed", pluginName)
	}
}

// testDockerDir returns the absolute path to test/docker/ containing the Dockerfile.
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
