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

// Plugin is the contract every bootstrap component must satisfy.
type Plugin interface {
	Name() string
	Description() string
	Dependencies() []string
	// Agents returns the provider names this plugin applies to.
	// An empty slice means the plugin applies to all providers.
	Agents() []string
	Check(ctx context.Context, env InstallEnv) (bool, error)
	Install(ctx context.Context, env InstallEnv) error
	Configure(ctx context.Context, env InstallEnv) error
}
