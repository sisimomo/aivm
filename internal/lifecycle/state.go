package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
)

// BootstrapVersion is incremented whenever host bootstrap-state schema or
// bootstrap behaviour changes. Stored in bootstrap-state.json on the host only.
const BootstrapVersion = "3"

// BootstrapState is persisted on the host (no SSH required) and records the
// provider and a hash of all execution-relevant configuration. Subsequent
// invocations compare the current hash against the saved one and skip bootstrap
// entirely when nothing has changed.
type BootstrapState struct {
	Version    string `json:"version"`
	Provider   string `json:"provider"`
	ConfigHash string `json:"config_hash"`
	EnvHash    string `json:"env_hash"`
}

func bootstrapStatePath(stateDir string) string {
	return filepath.Join(stateDir, "bootstrap-state.json")
}

func loadBootstrapState(stateDir string) (*BootstrapState, error) {
	data, err := os.ReadFile(bootstrapStatePath(stateDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s BootstrapState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil // treat corrupt state as absent
	}
	return &s, nil
}

func saveBootstrapState(stateDir string, s *BootstrapState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(bootstrapStatePath(stateDir), data, 0644)
}

func clearBootstrapState(stateDir string) {
	_ = os.Remove(bootstrapStatePath(stateDir))
}

// NeedsMigration reports whether the recorded schema version is outdated.
func (s *BootstrapState) NeedsMigration() bool {
	return s.Version != BootstrapVersion
}

// recordBootstrapState saves a BootstrapState with the current provider and
// config hash. Call this after any successful bootstrap.
func (svc *LifecycleService) recordBootstrapState() error {
	return saveBootstrapState(svc.Config.StateDir, &BootstrapState{
		Version:    BootstrapVersion,
		Provider:   svc.Provider.Name(),
		ConfigHash: svc.currentConfigHash(),
		EnvHash:    svc.currentEnvHash(),
	})
}

// currentEnvHash returns a deterministic SHA-256 hex digest of the resolved
// vm.env values. It is tracked separately from configHash so that env changes
// can be applied as a lightweight in-place update without recreating the VM.
func (svc *LifecycleService) currentEnvHash() string {
	env := svc.Config.VM.ResolvedEnv()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct{ K, V string }
	pairs := make([]kv, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, kv{k, env[k]})
	}
	data, _ := json.Marshal(pairs)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// currentConfigHash computes the hash covering all execution-relevant config.
func (svc *LifecycleService) currentConfigHash() string {
	enabled := BootstrapEnabledPlugins(svc.Registry, svc.EnabledProviders, svc.Config.Plugins.Enabled)
	return ComputeConfigHash(
		svc.PluginDefs,
		svc.Config.Plugins.Config,
		svc.Integrations,
		enabled,
		svc.Provider.Name(),
		svc.AgentDefs,
		svc.Config.VM.CPUs,
		svc.Config.VM.Memory,
		svc.Config.VM.Disk,
		svc.Config.VM.Type,
		svc.Config.VM.Mounts,
		svc.VM.Profile(),
	)
}

// ComputeConfigHash returns a SHA-256 hex digest of all execution-relevant
// configuration: provider, agent defs, VM resources, enabled plugin list,
// effective plugin defs, plugin config overrides, and integration defs.
//
// Nil slices and maps are normalized to empty collections before hashing. This
// ensures deterministic hashes across YAML deserialization variations, where
// Viper and mapstructure may produce nil vs empty depending on config presence.
// The function is insensitive to nil vs empty collections, performing inline
// normalization of vmMounts, integrations, and pluginConfig parameters.
//
// Excluded intentionally: MCP, T3Code, Idle timeouts, log_level, and VM
// prompt-threshold fields — none of these affect what runs inside the VM.
func ComputeConfigHash(
	pluginDefs map[string]plugin.PluginDef,
	pluginConfig map[string]map[string]any,
	integrations []integration.IntegrationDef,
	enabledPlugins []string,
	provider string,
	agentDefs map[string]agent.Def,
	vmCPUs int,
	vmMemory string,
	vmDisk string,
	vmType string,
	vmMounts []string,
	vmProfile string,
) string {
	sorted := append([]string(nil), enabledPlugins...)
	sort.Strings(sorted)

	// Normalise nil vs empty so that semantically equivalent values always
	// produce the same hash. Viper and mapstructure can deserialise the same
	// YAML differently (nil vs empty slice/map) depending on whether a key is
	// explicitly present in the config file. Without normalisation a user who
	// adds then removes `vm.mounts: []` (or `plugins.config: {}`) would see a
	// false-positive "VM config has changed" prompt on every run.
	if vmMounts == nil {
		vmMounts = []string{}
	}
	if integrations == nil {
		integrations = []integration.IntegrationDef{}
	}
	if pluginConfig == nil {
		pluginConfig = map[string]map[string]any{}
	}

	type hashInput struct {
		Provider       string
		AgentDefs      map[string]agent.Def
		VMCPUs         int
		VMMemory       string
		VMDisk         string
		VMType         string
		VMMounts       []string
		VMProfile      string
		EnabledPlugins []string
		PluginDefs     map[string]plugin.PluginDef
		PluginConfig   map[string]map[string]any
		Integrations   []integration.IntegrationDef
	}
	data, _ := json.Marshal(hashInput{
		Provider:       provider,
		AgentDefs:      agentDefs,
		VMCPUs:         vmCPUs,
		VMMemory:       vmMemory,
		VMDisk:         vmDisk,
		VMType:         vmType,
		VMMounts:       vmMounts,
		VMProfile:      vmProfile,
		EnabledPlugins: sorted,
		PluginDefs:     pluginDefs,
		PluginConfig:   pluginConfig,
		Integrations:   integrations,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
