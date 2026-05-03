package lifecycle

import (
	"context"
	"fmt"

	"aivm/internal/bootstrap"
	"aivm/internal/integration"
	aivmlog "aivm/internal/log"
	"aivm/internal/plugin"
	"aivm/internal/vm"
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
		},
		StateDir: svc.Config.StateDir,
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

// rebuildBootstrap runs every plugin unconditionally on v (force=true skips
// per-plugin Check). Does not update the host-side bootstrap state — the caller
// is responsible for that when appropriate.
func (svc *LifecycleService) rebuildBootstrap(ctx context.Context, v vm.VM) error {
	eng := svc.newBootstrapEngine(v, nil)
	if err := eng.Run(ctx, true); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return nil
}

// runIntegrationsFromState loads the current bootstrap state and executes any
// integrations that are newly applicable. It saves updated state when integrations run.
func (svc *LifecycleService) runIntegrationsFromState(ctx context.Context, targetVM vm.VM) error {
	if len(svc.Integrations) == 0 {
		return nil
	}
	state, err := loadBootstrapState(svc.Config.StateDir)
	if err != nil {
		aivmlog.Warn("could not read bootstrap state for integrations: %v", err)
		return nil
	}
	if state == nil {
		return nil
	}

	exec := &integration.Executor{
		Integrations:     svc.Integrations,
		InstalledPlugins: stringSet(state.AllInstalled()),
		ActiveAgents:     svc.Config.ActiveAgents(),
		AlreadyRan:       stringSet(state.AllIntegrations()),
		VM:               targetVM,
		Log:              aivmlog.Writer("integration"),
		TemplateVars: map[string]any{
			"mcp_port": fmt.Sprintf("%d", svc.Config.MCP.Port),
		},
	}

	matching := exec.Matching()
	if len(matching) == 0 {
		return nil
	}

	for _, integ := range matching {
		aivmlog.Step("Integration: %s → %s", integ.Key(), integ.To)
	}

	ran, err := exec.Run(ctx)
	if err != nil {
		return fmt.Errorf("running integrations: %w", err)
	}

	if len(ran) > 0 {
		state.MarkIntegrationRan(ran)
		if saveErr := saveBootstrapState(svc.Config.StateDir, state); saveErr != nil {
			aivmlog.Warn("could not save bootstrap state after integrations: %v", saveErr)
		}
	}
	return nil
}
