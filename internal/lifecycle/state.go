package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"

	"aivm/internal/bootstrap"
)

// BootstrapState is persisted on the host (no SSH required) and records which
// plugins have been installed in the current VM. Subsequent aivm invocations
// compare the desired plugin list against this state and skip bootstrap entirely
// when nothing has changed.
type BootstrapState struct {
	Version      string   `json:"version"`
	Provider     string   `json:"provider"`
	Installed    []string `json:"installed"`
	Integrations []string `json:"integrations,omitempty"` // "from:to" keys of executed integrations
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

func newBootstrapState(provider string, installed []string) *BootstrapState {
	return &BootstrapState{
		Version:   bootstrap.BootstrapVersion,
		Provider:  provider,
		Installed: installed,
	}
}

// NeedsMigration reports whether the recorded schema version is outdated.
func (s *BootstrapState) NeedsMigration() bool {
	return s.Version != bootstrap.BootstrapVersion
}

// IsInstalled reports whether a plugin has been recorded as installed.
func (s *BootstrapState) IsInstalled(name string) bool {
	for _, p := range s.Installed {
		if p == name {
			return true
		}
	}
	return false
}

// CurrentProvider returns the agent provider recorded in the state.
func (s *BootstrapState) CurrentProvider() string {
	return s.Provider
}

// AllInstalled returns the complete list of installed plugin names.
func (s *BootstrapState) AllInstalled() []string {
	return s.Installed
}

// AllIntegrations returns the complete list of integration keys that have run.
func (s *BootstrapState) AllIntegrations() []string {
	return s.Integrations
}

// MarkInstalled merges plugins into the installed list (deduplicates).
func (s *BootstrapState) MarkInstalled(plugins []string) {
	s.Installed = mergeStrings(s.Installed, plugins)
}

// MarkIntegrationRan merges keys into the integrations list (deduplicates).
func (s *BootstrapState) MarkIntegrationRan(keys []string) {
	s.Integrations = mergeStrings(s.Integrations, keys)
}

// SetProvider records the active agent provider name.
func (s *BootstrapState) SetProvider(name string) {
	s.Provider = name
}

// recordBootstrapState saves a BootstrapState reflecting the current desired
// plugin set for the service's provider. Call this after any successful full bootstrap.
func (svc *LifecycleService) recordBootstrapState() error {
	desired := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
	return saveBootstrapState(svc.Config.StateDir, newBootstrapState(svc.Provider.Name(), desired))
}
