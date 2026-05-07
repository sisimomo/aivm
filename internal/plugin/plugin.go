package plugin

import (
	"context"
	"io"
)

// VMRunner is the subset of vm.VM used by plugins during bootstrap.
type VMRunner interface {
	Run(ctx context.Context, script string, env map[string]string) error
}

// InstallEnv is passed to every plugin during bootstrap.
type InstallEnv struct {
	Config   map[string]any
	StateDir string
	Log      io.Writer
	DryRun   bool
	// VM is set during in-VM bootstrap; nil for host-side plugins.
	VM VMRunner
}

// ConfigString returns a string config value or the fallback.
func (e InstallEnv) ConfigString(key, fallback string) string {
	if v, ok := e.Config[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

// ConfigStringSlice returns a string slice config value, or nil if absent.
// Handles both []string (direct) and []interface{} (from YAML unmarshalling).
func (e InstallEnv) ConfigStringSlice(key string) []string {
	switch v := e.Config[key].(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// Plugin is the contract every bootstrap component must satisfy.
type Plugin interface {
	Name() string
	Description() string
	Dependencies() []string
	// Agents returns the provider names this plugin applies to.
	// An empty slice means the plugin applies to all providers.
	Agents() []string
	// PathEntries returns the PATH directories this plugin requires.
	// These are collected by the Executor and written to /etc/profile.d/aivm-path.sh
	// before any plugin setup runs.
	PathEntries() []string
	// SkipIf runs the skip_if script. Returns true when the plugin is already
	// set up and setup should be skipped (exit code 0 = skip).
	SkipIf(ctx context.Context, env InstallEnv) (bool, error)
	// Setup runs the combined install+configure script for this plugin.
	Setup(ctx context.Context, env InstallEnv) error
}
