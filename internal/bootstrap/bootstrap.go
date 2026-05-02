package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	aivmlog "aivm/internal/log"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

const markerFile = ".aivm-bootstrap-version"

// BootstrapVersion is incremented whenever the bootstrap schema changes.
// The CLI layer embeds this in the host-side bootstrap-state.json so it can
// detect when the schema has changed and trigger a fresh reconcile.
const BootstrapVersion = "2"

type Engine struct {
	VM       vm.VM
	Executor *plugin.Executor
	StateDir string
}

func (e *Engine) IsBootstrapped(ctx context.Context) bool {
	err := e.VM.Run(ctx, fmt.Sprintf(`[ -f ~/%s ] && cat ~/%s | grep -q '%s'`, markerFile, markerFile, BootstrapVersion), nil)
	return err == nil
}

func (e *Engine) Run(ctx context.Context, force bool) error {
	bootstrapped := e.IsBootstrapped(ctx)

	switch {
	case force:
		aivmlog.Step("Bootstrapping VM")
	case bootstrapped:
		aivmlog.Step("Reconciling VM bootstrap")
	default:
		aivmlog.Step("Bootstrapping VM")
	}

	ordered, err := e.Executor.Ordered()
	if err != nil {
		return fmt.Errorf("resolving plugin order: %w", err)
	}

	for _, p := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		aivmlog.Step("Plugin: %s", p.Name())

		cfg := e.Executor.PluginConfig[p.Name()]
		if cfg == nil {
			cfg = map[string]any{}
		}
		env := plugin.InstallEnv{
			Config:   cfg,
			StateDir: e.StateDir,
			Log:      aivmlog.Writer(p.Name()),
			VM:       e.VM,
		}

		// When force is set we are running on a fresh VM — skip idempotency
		// checks and always execute every plugin unconditionally.
		if !force {
			already, err := p.Check(ctx, env)
			if err != nil {
				aivmlog.Warn("check failed for plugin %s: %v", p.Name(), err)
			}
			if already {
				aivmlog.Info("skip %s (already installed)", p.Name())
				continue
			}
		}

		if err := p.Install(ctx, env); err != nil {
			return fmt.Errorf("install %s: %w", p.Name(), err)
		}
		if err := p.Configure(ctx, env); err != nil {
			return fmt.Errorf("configure %s: %w", p.Name(), err)
		}
		aivmlog.Success("%s installed", p.Name())
	}

	script := fmt.Sprintf(`echo '%s' > ~/%s`, BootstrapVersion, markerFile)
	if err := e.VM.Run(ctx, script, nil); err != nil {
		return fmt.Errorf("writing bootstrap marker: %w", err)
	}

	aivmlog.Success("Bootstrap complete!")
	return nil
}

func (e *Engine) LogPath() string {
	return filepath.Join(e.StateDir, "logs", "bootstrap.log")
}

func init() {
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".aivm", "logs"), 0755)
}
