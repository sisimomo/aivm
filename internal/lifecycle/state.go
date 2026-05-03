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

// recordBootstrapState saves a BootstrapState reflecting the current desired
// plugin set for the service's provider. Call this after any successful full bootstrap.
func (svc *LifecycleService) recordBootstrapState() error {
	desired := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
	return saveBootstrapState(svc.Config.StateDir, &BootstrapState{
		Version:   bootstrap.BootstrapVersion,
		Provider:  svc.Provider.Name(),
		Installed: desired,
	})
}
