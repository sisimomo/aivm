package lifecycle

import (
	"context"
	"fmt"

	"github.com/sisimomo/aivm/internal/bootstrap"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
)

// newBootstrapEngine builds a bootstrap.Engine targeting targetVM.
// plugins is an explicit list of plugins to enable; nil means all enabled
// plugins for the configured provider.
func (svc *LifecycleService) newBootstrapEngine(targetVM vm.VM, plugins []string) *bootstrap.Engine {
	enabled := plugins
	if enabled == nil {
		enabled = bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
	}
	return &bootstrap.Engine{
		VM: targetVM,
		Executor: &plugin.Executor{
			Registry:     svc.Registry,
			Enabled:      enabled,
			PluginConfig: svc.Config.Plugins.Config,
			StateDir:     svc.Config.StateDir,
			VMInst:       targetVM,
			Log:          svc.log(),
		},
		StateDir: svc.Config.StateDir,
		Log:      svc.log(),
	}
}

// fullBootstrap runs all configured plugins on targetVM and saves bootstrap
// state on success. Use force=true for a fresh blank VM; force=false for an
// existing VM with unknown state (uses Check to skip already-installed plugins).
func (svc *LifecycleService) fullBootstrap(ctx context.Context, targetVM vm.VM, force bool) error {
	eng := svc.newBootstrapEngine(targetVM, nil)
	if err := eng.Run(ctx, force); err != nil {
		return err
	}
	if err := svc.recordBootstrapState(); err != nil {
		return err
	}
	return svc.runIntegrationsFromState(ctx, targetVM)
}

// runIntegrationsFromState executes integrations whose From/To conditions are
// satisfied. InstalledPlugins is derived from the current enabled plugin list
// (all enabled plugins are considered set up after a successful full bootstrap).
// Each integration's skip_if script gates whether it actually runs.
func (svc *LifecycleService) runIntegrationsFromState(ctx context.Context, targetVM vm.VM) error {
	if len(svc.Integrations) == 0 {
		return nil
	}

	enabledPlugins := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)

	exec := &integration.Executor{
		Integrations:     svc.Integrations,
		InstalledPlugins: stringSet(enabledPlugins),
		ActiveAgents:     svc.Config.ActiveAgents(),
		VM:               targetVM,
		Log:              svc.log().Writer("integration"),
		TemplateVars: map[string]any{
			"mcp_port": fmt.Sprintf("%d", svc.Config.MCP.Port),
		},
	}

	matching := exec.Matching()
	if len(matching) == 0 {
		return nil
	}

	for _, integ := range matching {
		svc.log().Step("Integration: %s → %s", integ.Key(), integ.To)
	}

	if _, err := exec.Run(ctx); err != nil {
		return fmt.Errorf("running integrations: %w", err)
	}
	return nil
}
