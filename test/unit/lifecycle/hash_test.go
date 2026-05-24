package lifecycle_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/providers/generic"
)

// minimalAgentYAML returns a minimal aivm.yaml that enables the named agent,
// written to a temp file. The caller should defer os.Remove on the returned path.
func minimalAgentYAML(t *testing.T, agentName string) string {
	t.Helper()
	content := "agents:\n  default: " + agentName + "\n  define:\n    " + agentName + ":\n      enable: true\n"
	f, err := os.CreateTemp(t.TempDir(), "aivm-hash-test-*.yaml")
	if err != nil {
		t.Fatalf("creating temp config: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	f.Close()
	return f.Name()
}

// buildHashInputsFromCompose simulates exactly what buildApp + Compose do to
// populate the fields that currentConfigHash reads. Each call starts from
// scratch so two calls model two independent process invocations.
func buildHashInputsFromCompose(t *testing.T, providerName string) (
	pluginDefs map[string]plugin.PluginDef,
	enabledAgentDefs map[string]agent.Def,
	integrations []integration.IntegrationDef,
) {
	t.Helper()

	// --- mirror buildApp ---
	rawAgentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("agent.LoadDefs: %v", err)
	}
	agentReg := agent.NewRegistry()
	for name, def := range rawAgentDefs {
		agentReg.Register(generic.NewFromDef(name, def))
	}

	// Write a minimal config file so Compose finds an enabled agent.
	cfgPath := minimalAgentYAML(t, providerName)

	// --- mirror Compose ---
	engine := &config.CompositionEngine{Defaults: config.Defaults{StateDir: "~/.aivm"}}
	result, err := engine.Compose(cfgPath, agentReg)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	// --- mirror buildApp post-Compose field assignments ---
	pluginDefs = result.PluginDefs
	enabledAgentDefs = result.EnabledAgentDefs
	integrations = result.Integrations
	return
}

// writeMinimalAgentConfig writes a minimal aivm.yaml enabling the named agent
// to the given path. Used to produce a stable cfgPath for hash comparison tests.
func writeMinimalAgentConfig(t *testing.T, path, agentName string) {
	t.Helper()
	content := "agents:\n  default: " + agentName + "\n  define:\n    " + agentName + ":\n      enable: true\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeMinimalAgentConfig: %v", err)
	}
}

// TestComputeConfigHash_StableAcrossSimulatedRuns is the key regression test:
// it verifies that ComputeConfigHash produces identical output when called
// twice from independently loaded inputs — exactly as two separate process
// invocations would behave. A failure here is the direct cause of the
// false-positive "VM config has changed" prompt on every aivm run.
func TestComputeConfigHash_StableAcrossSimulatedRuns(t *testing.T) {
	const provider = "claude"

	// Use a fixed path so both runs load from the same file.
	cfgPath := filepath.Join(t.TempDir(), "aivm.yaml")
	writeMinimalAgentConfig(t, cfgPath, provider)

	buildInputs := func() (map[string]plugin.PluginDef, map[string]agent.Def, []integration.IntegrationDef) {
		rawAgentDefs, err := agent.LoadDefs()
		if err != nil {
			t.Fatalf("agent.LoadDefs: %v", err)
		}
		agentReg := agent.NewRegistry()
		for name, def := range rawAgentDefs {
			agentReg.Register(generic.NewFromDef(name, def))
		}
		engine := &config.CompositionEngine{Defaults: config.Defaults{StateDir: "~/.aivm"}}
		result, err := engine.Compose(cfgPath, agentReg)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		return result.PluginDefs, result.EnabledAgentDefs, result.Integrations
	}

	// Simulate run 1: build all service inputs from scratch, then hash.
	pluginDefs1, agentDefs1, integs1 := buildInputs()
	h1 := lifecycle.ComputeConfigHash(
		pluginDefs1,
		nil, // no plugins.config in default aivm.yaml
		integs1,
		[]string{"system", "mise-node", "mise-python", "mise-uv", "claude"},
		provider,
		agentDefs1,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm",
	)

	// Simulate run 2: load ALL inputs completely from scratch again.
	pluginDefs2, agentDefs2, integs2 := buildInputs()
	h2 := lifecycle.ComputeConfigHash(
		pluginDefs2,
		nil,
		integs2,
		[]string{"system", "mise-node", "mise-python", "mise-uv", "claude"},
		provider,
		agentDefs2,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm",
	)

	if h1 != h2 {
		// Emit the JSON that feeds into the hash so the diff is actionable.
		j1, _ := json.MarshalIndent(pluginDefs1, "", "  ")
		j2, _ := json.MarshalIndent(pluginDefs2, "", "  ")
		t.Errorf("hash is not stable across simulated runs:\n  run1 = %s\n  run2 = %s\npluginDefs run1:\n%s\npluginDefs run2:\n%s",
			h1, h2, j1, j2)
	}
}

// TestComputeConfigHash_SameInputsSameHash is a simpler sanity check:
// the same arguments must always produce the same result within one process.
func TestComputeConfigHash_SameInputsSameHash(t *testing.T) {
	pluginDefs, agentDefs, integs := buildHashInputsFromCompose(t, "claude")

	h1 := lifecycle.ComputeConfigHash(pluginDefs, nil, integs,
		[]string{"claude", "system"}, "claude", agentDefs,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm")

	h2 := lifecycle.ComputeConfigHash(pluginDefs, nil, integs,
		[]string{"claude", "system"}, "claude", agentDefs,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm")

	if h1 != h2 {
		t.Errorf("same inputs produced different hashes: %s vs %s", h1, h2)
	}
}

// TestComputeConfigHash_EnabledPluginsOrderIndependent verifies that the hash
// is the same regardless of the order in which enabledPlugins are supplied,
// since they are sorted internally.
func TestComputeConfigHash_EnabledPluginsOrderIndependent(t *testing.T) {
	pluginDefs, agentDefs, integs := buildHashInputsFromCompose(t, "claude")

	h1 := lifecycle.ComputeConfigHash(pluginDefs, nil, integs,
		[]string{"system", "claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm")

	h2 := lifecycle.ComputeConfigHash(pluginDefs, nil, integs,
		[]string{"claude", "system"}, "claude", agentDefs,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm")

	if h1 != h2 {
		t.Errorf("hash should be order-independent for enabledPlugins: %s vs %s", h1, h2)
	}
}

// TestComputeConfigHash_MultiAgentStableAcrossRuns verifies that a config with
// two enabled agents produces an identical hash across two independent
// compositions — the multi-agent equivalent of the single-agent stability test.
func TestComputeConfigHash_MultiAgentStableAcrossRuns(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aivm-multi.yaml")
	content := "agents:\n  default: claude\n  define:\n    claude:\n      enable: true\n    copilot:\n      enable: true\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	buildMultiInputs := func() (map[string]plugin.PluginDef, map[string]agent.Def, []integration.IntegrationDef) {
		rawAgentDefs, err := agent.LoadDefs()
		if err != nil {
			t.Fatalf("agent.LoadDefs: %v", err)
		}
		agentReg := agent.NewRegistry()
		for name, def := range rawAgentDefs {
			agentReg.Register(generic.NewFromDef(name, def))
		}
		engine := &config.CompositionEngine{Defaults: config.Defaults{StateDir: "~/.aivm"}}
		result, err := engine.Compose(cfgPath, agentReg)
		if err != nil {
			t.Fatalf("Compose: %v", err)
		}
		return result.PluginDefs, result.EnabledAgentDefs, result.Integrations
	}

	pluginDefs1, agentDefs1, integs1 := buildMultiInputs()
	h1 := lifecycle.ComputeConfigHash(
		pluginDefs1, nil, integs1,
		[]string{"system", "mise-node", "mise-python", "mise-uv", "claude"},
		"claude", agentDefs1,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm",
	)

	pluginDefs2, agentDefs2, integs2 := buildMultiInputs()
	h2 := lifecycle.ComputeConfigHash(
		pluginDefs2, nil, integs2,
		[]string{"system", "mise-node", "mise-python", "mise-uv", "claude"},
		"claude", agentDefs2,
		4, "8GB", "60GB", "", []string{"~/dev:rw"}, "aivm",
	)

	if h1 != h2 {
		j1, _ := json.MarshalIndent(agentDefs1, "", "  ")
		j2, _ := json.MarshalIndent(agentDefs2, "", "  ")
		t.Errorf("multi-agent hash is not stable across runs:\n  run1 = %s\n  run2 = %s\nagentDefs run1:\n%s\nagentDefs run2:\n%s",
			h1, h2, j1, j2)
	}
}

// TestComputeConfigHash_NilVsEmptySlicesAreNormalised documents the expected
// behaviour for nil vs empty slice inputs that arise naturally from YAML loading:
// both nil and an empty slice must hash identically so that a hash saved on
// one run (where a field happened to be nil) matches the hash on a subsequent
// run (where the same field is an empty slice due to Viper/mapstructure
// zero-value differences).
func TestComputeConfigHash_NilVsEmptySlicesAreNormalised(t *testing.T) {
	pluginDefs, agentDefs, _ := buildHashInputsFromCompose(t, "claude")

	// VMMounts: nil vs empty
	hNilMounts := lifecycle.ComputeConfigHash(pluginDefs, nil, nil,
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", nil, "aivm")
	hEmptyMounts := lifecycle.ComputeConfigHash(pluginDefs, nil, nil,
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", []string{}, "aivm")
	if hNilMounts != hEmptyMounts {
		t.Errorf("nil VMMounts and empty VMMounts must produce the same hash:\n  nil   = %s\n  empty = %s",
			hNilMounts, hEmptyMounts)
	}

	// Integrations: nil vs empty
	hNilInteg := lifecycle.ComputeConfigHash(pluginDefs, nil, nil,
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", nil, "aivm")
	hEmptyInteg := lifecycle.ComputeConfigHash(pluginDefs, nil, []integration.IntegrationDef{},
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", nil, "aivm")
	if hNilInteg != hEmptyInteg {
		t.Errorf("nil Integrations and empty Integrations must produce the same hash:\n  nil   = %s\n  empty = %s",
			hNilInteg, hEmptyInteg)
	}

	// PluginConfig: nil vs empty map
	hNilCfg := lifecycle.ComputeConfigHash(pluginDefs, nil, nil,
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", nil, "aivm")
	hEmptyCfg := lifecycle.ComputeConfigHash(pluginDefs, map[string]map[string]any{}, nil,
		[]string{"claude"}, "claude", agentDefs,
		4, "8GB", "60GB", "", nil, "aivm")
	if hNilCfg != hEmptyCfg {
		t.Errorf("nil PluginConfig and empty PluginConfig must produce the same hash:\n  nil   = %s\n  empty = %s",
			hNilCfg, hEmptyCfg)
	}
}
