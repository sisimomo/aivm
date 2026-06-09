package lifecycle

import (
	"context"
	"fmt"
	"io"

	"github.com/sisimomo/aivm/internal/integration"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
)

// bootstrap runs all configured plugins on targetVM and saves bootstrap state on success.
func (svc *LifecycleService) bootstrap(ctx context.Context, targetVM vm.VM) error {
	svc.logger().Info("Bootstrapping VM")

	exec := &plugin.Executor{
		Registry:     svc.Registry,
		Enabled:      BootstrapEnabledPlugins(svc.Registry, svc.EnabledProviders, svc.Config.Plugins.Enabled),
		PluginConfig: svc.Config.Plugins.Config,
		StateDir:     svc.Config.StateDir,
		VMInst:       targetVM,
	}
	if err := exec.Run(ctx); err != nil {
		return err
	}
	if _, ok := targetVM.(*vm.LimaVM); ok {
		vm.CloseSSHControlMaster(ctx, targetVM.Profile())
	}
	svc.logger().Info("Bootstrap complete!")
	if err := applyVMEnv(ctx, targetVM, svc.Config.VM.ResolvedEnv()); err != nil {
		return fmt.Errorf("applying vm.env: %w", err)
	}
	gitName, gitEmail := readHostGitIdentity()
	if err := applyGitIdentity(ctx, targetVM, gitName, gitEmail); err != nil {
		return fmt.Errorf("applying git identity: %w", err)
	}
	if err := svc.recordBootstrapState(); err != nil {
		return err
	}
	return svc.runIntegrationsFromState(ctx, targetVM)
}

// runIntegrationsFromState executes integrations whose From/To conditions are
// satisfied. InstalledPlugins is derived from the current enabled plugin list
// (all enabled plugins are considered set up after a successful bootstrap).
func (svc *LifecycleService) runIntegrationsFromState(ctx context.Context, targetVM vm.VM) error {
	if len(svc.Integrations) == 0 {
		return nil
	}

	enabledPlugins := BootstrapEnabledPlugins(svc.Registry, svc.EnabledProviders, svc.Config.Plugins.Enabled)

	return aivmlog.WithWriter("integration", func(logW io.Writer) error {
		exec := &integration.Executor{
			Integrations:     svc.Integrations,
			InstalledPlugins: stringSet(enabledPlugins),
			ActiveAgents:     svc.Config.ActiveAgents(),
			VM:               targetVM,
			Log:              logW,
			TemplateVars:     map[string]any{},
		}

		matching := exec.Matching()
		if len(matching) == 0 {
			return nil
		}

		for _, integ := range matching {
			svc.logger().Info(fmt.Sprintf("Integration: %s → %s", integ.Key(), integ.To))
		}

		if _, err := exec.Run(ctx); err != nil {
			return fmt.Errorf("running integrations: %w", err)
		}
		return nil
	})
}
