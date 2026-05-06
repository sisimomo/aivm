//go:build bootstrap

package bootstraptest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
)

const maxConcurrentBootstrapHarnesses = 5

var bootstrapHarnessSem = make(chan struct{}, maxConcurrentBootstrapHarnesses)

// BootstrapHarness is a test helper that runs real plugin install scripts,
// agent setups, and integrations inside a fresh Docker container. It uses the
// real plugin.Executor and integration.Executor (no stubs) so template
// rendering, dependency ordering, and the full bootstrap logic are exercised.
type BootstrapHarness struct {
	t                *testing.T
	vm               *framework.DockerVM
	reg              *plugin.Registry
	defs             map[string]plugin.PluginDef
	integDefs        []integration.IntegrationDef
	installedPlugins map[string]bool
	stateDir         string
}

// newBootstrapHarness builds a fresh Docker container and loads all real
// plugin, agent, and integration definitions. The container and temp state dir
// are removed automatically when the test ends.
func newBootstrapHarness(t *testing.T) *BootstrapHarness {
	t.Helper()
	acquireBootstrapHarness(t)

	if err := framework.EnsureTestImage(testDockerDir()); err != nil {
		t.Fatalf("harness: ensure test image: %v", err)
	}

	suffix := mustRandomHex(6)
	profile := "aivm-bstest-" + suffix

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

	// Load integration definitions.
	integDefs, err := integration.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load integration defaults: %v", err)
	}

	h := &BootstrapHarness{
		t:                t,
		vm:               dockerVM,
		reg:              reg,
		defs:             defs,
		integDefs:        integDefs,
		installedPlugins: make(map[string]bool),
		stateDir:         stateDir,
	}

	t.Cleanup(func() {
		_ = dockerVM.Destroy(context.Background())
		os.RemoveAll(testRunDir) //nolint:errcheck
	})

	return h
}

func acquireBootstrapHarness(t *testing.T) {
	t.Helper()
	bootstrapHarnessSem <- struct{}{}
	t.Cleanup(func() {
		<-bootstrapHarnessSem
	})
}

// Install runs the named plugin (and all its transitive dependencies) inside
// the container using the real bootstrap engine. cfg overrides per-plugin
// config for the plugin under test; dependencies use their own defaults.
//
// All transitively installed plugins are recorded so that a subsequent
// RunIntegrations call can accurately match From conditions.
func (h *BootstrapHarness) Install(pluginName string, cfg map[string]any) {
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

	// Resolve the full execution order before running so we can record every
	// installed plugin (including transitive deps) for later RunIntegrations.
	ordered, err := exec.Ordered()
	if err != nil {
		h.t.Fatalf("Install %q: resolve order: %v", pluginName, err)
	}

	if err := exec.Run(context.Background(), true); err != nil {
		h.t.Fatalf("Install %q: %v", pluginName, err)
	}

	for _, p := range ordered {
		h.installedPlugins[p.Name()] = true
	}
}

// RunIntegrations executes all integrations from the embedded defaults whose
// From/To conditions match the plugins installed so far and the given active
// agent. templateVars are substituted into configure and skip_if scripts
// (e.g. map[string]any{"mcp_port": "8472"}).
func (h *BootstrapHarness) RunIntegrations(agentName string, templateVars map[string]any) {
	h.t.Helper()

	exec := &integration.Executor{
		Integrations:     h.integDefs,
		InstalledPlugins: h.installedPlugins,
		ActiveAgents:     []string{agentName},
		VM:               h.vm,
		Log:              os.Stderr,
		TemplateVars:     templateVars,
	}

	if _, err := exec.Run(context.Background()); err != nil {
		h.t.Fatalf("RunIntegrations %q: %v", agentName, err)
	}
}

// AssertIntegrationConfigured runs the skip_if script of the named integration
// (matched by key) and asserts that it exits 0, confirming the integration has
// been applied. templateVars must match those passed to RunIntegrations.
//
// The integration must declare a non-empty skip_if script; if it does not,
// the test fails with a clear message directing callers to use AssertCommand.
func (h *BootstrapHarness) AssertIntegrationConfigured(integKey string, templateVars map[string]any) {
	h.t.Helper()

	var found *integration.IntegrationDef
	for i := range h.integDefs {
		if h.integDefs[i].Key() == integKey {
			found = &h.integDefs[i]
			break
		}
	}
	if found == nil {
		h.t.Fatalf("AssertIntegrationConfigured: integration %q not found", integKey)
		return
	}
	if found.SkipIf == "" {
		h.t.Fatalf("AssertIntegrationConfigured: integration %q has no skip_if — use AssertCommand to verify its effect instead", integKey)
		return
	}

	script, err := renderIntegrationScript(found.SkipIf, templateVars)
	if err != nil {
		h.t.Fatalf("AssertIntegrationConfigured %q: render skip_if: %v", integKey, err)
		return
	}

	if err := h.vm.Run(context.Background(), script, nil); err != nil {
		h.t.Fatalf("AssertIntegrationConfigured %q: skip_if exited non-zero — integration not applied\n%v", integKey, err)
	}
}

// AssertCommand runs cmd inside the container as a login shell and asserts that
// the combined output contains wantSubstr. Fails the test on error or mismatch.
func (h *BootstrapHarness) AssertCommand(cmd, wantSubstr string) {
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
func (h *BootstrapHarness) AssertSkipIf(pluginName string, cfg map[string]any) {
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

// renderIntegrationScript renders a Go text/template integration script with
// the given data map, matching the logic used by integration.Executor.
func renderIntegrationScript(src string, data map[string]any) (string, error) {
	t, err := template.New("").Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
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
