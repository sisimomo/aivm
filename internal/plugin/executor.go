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
}

// Ordered returns the enabled plugins in topological order.
func (e *Executor) Ordered() ([]Plugin, error) {
	return e.Registry.Resolve(e.Enabled)
}

func (e *Executor) Run(ctx context.Context) error {
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
			Log:      aivmlog.Writer(p.Name()),
			VM:       e.VMInst,
		}

		already, err := p.Check(ctx, env)
		if err != nil {
			aivmlog.Warn("check failed for %s: %v", p.Name(), err)
		}
		if already {
			aivmlog.Info("skip %-12s (already installed)", p.Name())
			continue
		}

		aivmlog.Step("Installing %s", p.Name())
		start := time.Now()

		if err := p.Install(ctx, env); err != nil {
			return fmt.Errorf("install %s: %w", p.Name(), err)
		}
		if err := p.Configure(ctx, env); err != nil {
			return fmt.Errorf("configure %s: %w", p.Name(), err)
		}

		aivmlog.Success("%s installed (%s)", p.Name(), time.Since(start).Round(time.Second))
	}
	return nil
}
