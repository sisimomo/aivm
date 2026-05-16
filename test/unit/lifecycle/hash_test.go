package lifecycle_test

import (
	"encoding/json"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/providers/generic"
)

// buildHashInputsFromCompose simulates exactly what buildApp + Compose do to
// populate the fields that currentConfigHash reads. Each call starts from
// scratch so two calls model two independent process invocations.
func buildHashInputsFromCompose(t *testing.T, providerName string) (
	pluginDefs map[string]plugin.PluginDef,
	activeAgentDefs map[string]agent.Def,
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

	// --- mirror Compose ---
	engine := &config.CompositionEngine{Defaults: config.Defaults{StateDir: "~/.aivm"}}
	// Pass an empty cfgPath so Load uses only defaults (no user aivm.yaml).
	result, err := engine.Compose("", agentReg)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	// --- mirror buildApp post-Compose field assignments ---
	pluginDefs = result.PluginDefs
	activeAgentDefs = map[string]agent.Def{result.ActiveProvider.Name(): result.ActiveAgentDef}
	integrations = result.Integrations
	return
}

// TestComputeConfigHash_StableAcrossSimulatedRuns is the key regression test:
// it verifies that ComputeConfigHash produces identical output when called
// twice from independently loaded inputs — exactly as two separate process
// invocations would behave. A failure here is the direct cause of the
// false-positive "VM config has changed" prompt on every aivm run.
func TestComputeConfigHash_StableAcrossSimulatedRuns(t *testing.T) {
	const provider = "claude"

	// Simulate run 1: build all service inputs from scratch, then hash.
	pluginDefs1, agentDefs1, integs1 := buildHashInputsFromCompose(t, provider)
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
	pluginDefs2, agentDefs2, integs2 := buildHashInputsFromCompose(t, provider)
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
