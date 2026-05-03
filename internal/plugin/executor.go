package plugin

import (
	"context"
	"fmt"
	"time"

	aivmlog "aivm/internal/log"
)

// Executor runs plugins in DAG order.
type Executor struct {
	Registry     *Registry
	Enabled      []string
	PluginConfig map[string]map[string]any
	StateDir     string
	// VMInst is passed as InstallEnv.VM so plugins can run commands in the VM.
	VMInst VMRunner
	// Log receives all user-visible output. When nil, aivmlog.Default is used.
	// Inject a custom logger in tests to capture output.
	Log *aivmlog.Logger
}

func (e *Executor) log() *aivmlog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return aivmlog.Default
}

// Ordered returns the enabled plugins in topological order.
func (e *Executor) Ordered() ([]Plugin, error) {
	return e.Registry.Resolve(e.Enabled)
}

// Run executes all enabled plugins in DAG order.
//
// When force is true every plugin's Install+Configure steps run unconditionally
// (Check is skipped). Use force=true on a fresh blank VM; force=false on an
// existing VM so already-installed plugins are skipped.
func (e *Executor) Run(ctx context.Context, force bool) error {
	ordered, err := e.Ordered()
	if err != nil {
		return fmt.Errorf("resolving plugin order: %w", err)
	}

	for _, p := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}

		cfg := e.PluginConfig[p.Name()]
		if cfg == nil {
			cfg = map[string]any{}
		}
		env := InstallEnv{
			Config:   cfg,
			StateDir: e.StateDir,
			Log:      e.log().Writer(p.Name()),
			VM:       e.VMInst,
		}

		if !force {
			already, err := p.Check(ctx, env)
			if err != nil {
				e.log().Warn("check failed for plugin %s: %v", p.Name(), err)
			}
			if already {
				e.log().Info("skip %s (already installed)", p.Name())
				continue
			}
		}

		e.log().Step("Plugin: %s", p.Name())
		start := time.Now()

		if err := p.Install(ctx, env); err != nil {
			return fmt.Errorf("install %s: %w", p.Name(), err)
		}
		if err := p.Configure(ctx, env); err != nil {
			return fmt.Errorf("configure %s: %w", p.Name(), err)
		}

		e.log().Success("%s installed (%s)", p.Name(), time.Since(start).Round(time.Second))
	}
	return nil
}
