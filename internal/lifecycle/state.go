package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"aivm/internal/bootstrap"
	"aivm/internal/integration"
	"aivm/internal/plugin"
)

// BootstrapState is persisted on the host (no SSH required) and records the
// provider and a hash of all execution-relevant configuration. Subsequent
// invocations compare the current hash against the saved one and skip bootstrap
// entirely when nothing has changed.
type BootstrapState struct {
	Version    string `json:"version"`
	Provider   string `json:"provider"`
	ConfigHash string `json:"config_hash"`
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
	return s.Version != bootstrap.BootstrapVersion
}

// recordBootstrapState saves a BootstrapState with the current provider and
// config hash. Call this after any successful full bootstrap.
func (svc *LifecycleService) recordBootstrapState() error {
	return saveBootstrapState(svc.Config.StateDir, &BootstrapState{
		Version:    bootstrap.BootstrapVersion,
		Provider:   svc.Provider.Name(),
		ConfigHash: svc.currentConfigHash(),
	})
}

// currentConfigHash computes the hash covering all execution-relevant config.
func (svc *LifecycleService) currentConfigHash() string {
	enabled := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
	return computeConfigHash(svc.PluginDefs, svc.Config.Plugins.Config, svc.Integrations, enabled, svc.Provider.Name())
}

// computeConfigHash returns a SHA-256 hex digest of all execution-relevant
// configuration: enabled plugin list, effective plugin defs, plugin config
// overrides, integration defs, and active provider name.
func computeConfigHash(
	pluginDefs map[string]plugin.PluginDef,
	pluginConfig map[string]map[string]any,
	integrations []integration.IntegrationDef,
	enabledPlugins []string,
	provider string,
) string {
	sorted := append([]string(nil), enabledPlugins...)
	sort.Strings(sorted)

	type hashInput struct {
		Provider       string
		EnabledPlugins []string
		PluginDefs     map[string]plugin.PluginDef
		PluginConfig   map[string]map[string]any
		Integrations   []integration.IntegrationDef
	}
	data, _ := json.Marshal(hashInput{
		Provider:       provider,
		EnabledPlugins: sorted,
		PluginDefs:     pluginDefs,
		PluginConfig:   pluginConfig,
		Integrations:   integrations,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
