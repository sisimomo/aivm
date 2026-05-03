package lifecycle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"aivm/internal/bootstrap"
	aivmlog "aivm/internal/log"
	"aivm/internal/vm"
)

// syncStep represents one decision point in the bootstrap state machine.
type syncStep interface {
	applicable(ss *syncState, svc *LifecycleService) bool
	run(ctx context.Context, ss *syncState, svc *LifecycleService) error
}

type syncState struct {
	state   *BootstrapState
	desired []string
}

// syncPipeline is the ordered list of steps evaluated by syncBootstrap.
// The first applicable step is executed; subsequent steps are skipped.
var syncPipeline = []syncStep{
	&missingOrStaleStep{},
	&agentMismatchStep{},
	&newPluginsStep{},
	&upToDateStep{},
}

// syncBootstrap is the main bootstrap entry point on every aivm invocation.
// It reads the host-side state file (no SSH) and returns immediately when
// nothing has changed, runs only newly-added plugins when the plugin list
// grew, or handles an agent mismatch interactively.
func (svc *LifecycleService) syncBootstrap(ctx context.Context) error {
	state, err := loadBootstrapState(svc.Config.StateDir)
	if err != nil {
		aivmlog.Warn("could not read bootstrap state, running full bootstrap: %v", err)
	}
	desired := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
	ss := &syncState{state: state, desired: desired}

	for _, step := range syncPipeline {
		if step.applicable(ss, svc) {
			return step.run(ctx, ss, svc)
		}
	}
	return nil
}

// missingOrStaleStep runs a full bootstrap when there is no state or the
// schema version is outdated.
type missingOrStaleStep struct{}

func (s *missingOrStaleStep) applicable(ss *syncState, _ *LifecycleService) bool {
	return ss.state == nil || ss.state.Version != bootstrap.BootstrapVersion
}

func (s *missingOrStaleStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
	return svc.fullBootstrap(ctx, svc.VM, false)
}

// agentMismatchStep handles the case where the configured agent is not installed
// but a different agent is — prompts the user to install the new agent or recreate.
type agentMismatchStep struct{}

func (s *agentMismatchStep) applicable(ss *syncState, svc *LifecycleService) bool {
	if ss.state == nil || requiredPluginsInstalled(svc.Provider, ss.state.Installed) {
		return false
	}
	installed := installedProvidersFromState(svc, ss.state)
	delete(installed, svc.Provider.Name())
	return len(installed) > 0
}

func (s *agentMismatchStep) run(ctx context.Context, ss *syncState, svc *LifecycleService) error {
	installed := installedProvidersFromState(svc, ss.state)
	delete(installed, svc.Provider.Name())
	return svc.resolveAgentMismatch(ctx, ss.state, installed)
}

// newPluginsStep installs plugins that have been added since the last recorded
// bootstrap state.
type newPluginsStep struct{}

func (s *newPluginsStep) applicable(ss *syncState, _ *LifecycleService) bool {
	if ss.state == nil {
		return false
	}
	installedSet := stringSet(ss.state.Installed)
	for _, p := range ss.desired {
		if !installedSet[p] {
			return true
		}
	}
	return false
}

func (s *newPluginsStep) run(ctx context.Context, ss *syncState, svc *LifecycleService) error {
	installedSet := stringSet(ss.state.Installed)
	var newPlugins []string
	for _, p := range ss.desired {
		if !installedSet[p] {
			newPlugins = append(newPlugins, p)
		}
	}
	aivmlog.Step("Installing %d new plugin(s): %s", len(newPlugins), strings.Join(newPlugins, ", "))
	eng := svc.newBootstrapEngine(svc.VM, newPlugins)
	if err := eng.Run(ctx, false); err != nil {
		return err
	}
	ss.state.Installed = mergeStrings(ss.state.Installed, newPlugins)
	ss.state.Provider = svc.Provider.Name()
	if err := saveBootstrapState(svc.Config.StateDir, ss.state); err != nil {
		return err
	}
	return svc.runIntegrationsFromState(ctx, svc.VM)
}

// upToDateStep is the terminal fallthrough: bootstrap state is current, so only
// integrations need to be checked.
type upToDateStep struct{}

func (s *upToDateStep) applicable(ss *syncState, _ *LifecycleService) bool {
	return ss.state != nil
}

func (s *upToDateStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
	aivmlog.Info("VM is up to date — skipping bootstrap")
	return svc.runIntegrationsFromState(ctx, svc.VM)
}

// resolveAgentMismatch handles the case where the VM has a different agent than
// the configured one. Prompts the user to install the new agent or recreate the VM.
func (svc *LifecycleService) resolveAgentMismatch(ctx context.Context, state *BootstrapState, otherInstalled map[string]bool) error {
	installedDescriptions := installedProviderDescriptions(svc, otherInstalled)
	configured := svc.Provider.Description()
	installedSummary := strings.Join(installedDescriptions, ", ")

	aivmlog.Warn("VM '%s' was created for a different agent", svc.VM.Profile())
	if len(installedDescriptions) == 1 {
		aivmlog.Warn("Installed agent: %s", installedSummary)
	} else {
		aivmlog.Warn("Installed agents: %s", installedSummary)
	}
	aivmlog.Warn("Configured agent: %s", configured)

	if !svc.Confirmer.IsInteractive() {
		return fmt.Errorf(
			"VM %q was created for %s, but config selects %s; rerun interactively to choose whether to install %s into the existing VM or recreate it with only %s",
			svc.VM.Profile(),
			installedSummary,
			configured,
			configured,
			configured,
		)
	}

	sessions, _ := svc.Sessions.List()
	decision, ok := promptAgentMismatch(svc.Confirmer, installedSummary, configured, len(sessions))
	if !ok {
		return fmt.Errorf("invalid choice")
	}

	switch decision {
	case agentMismatchInstall:
		desired := bootstrapEnabledPlugins(svc.Registry, svc.Provider, svc.Config.Plugins.Enabled)
		installedSet := stringSet(state.Installed)
		var newPlugins []string
		for _, p := range desired {
			if !installedSet[p] {
				newPlugins = append(newPlugins, p)
			}
		}
		eng := svc.newBootstrapEngine(svc.VM, newPlugins)
		if err := eng.Run(ctx, false); err != nil {
			return err
		}
		state.Installed = mergeStrings(state.Installed, newPlugins)
		state.Provider = svc.Provider.Name()
		if err := saveBootstrapState(svc.Config.StateDir, state); err != nil {
			return err
		}
		return svc.runIntegrationsFromState(ctx, svc.VM)
	case agentMismatchRecreate:
		return svc.recreateVMForConfiguredAgent(ctx)
	default:
		return fmt.Errorf("invalid choice")
	}
}

// recreateVMForConfiguredAgent terminates all active sessions, destroys the VM,
// recreates it with a fresh bootstrap, and saves a new base image.
func (svc *LifecycleService) recreateVMForConfiguredAgent(ctx context.Context) error {
	sessions, _ := svc.Sessions.List()
	if len(sessions) > 0 {
		aivmlog.Step("Terminating %d active session(s)", len(sessions))
		for _, sess := range sessions {
			proc, err := os.FindProcess(sess.PID)
			if err == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
			sess.Remove()
		}
	}

	clearBootstrapState(svc.Config.StateDir)

	aivmlog.Step("Recreating VM for %s", svc.Provider.Description())
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	imgMgr := vm.NewImageManager(svc.VM, svc.Config.StateDir)
	if _, err := svc.bootstrapFreshVM(ctx, svc.VM, imgMgr); err != nil {
		return err
	}

	svc.Sessions.ClearVMStoppedAt()
	vm.ClearTransitionState(svc.Config.StateDir)

	aivmlog.Success("VM recreated with only %s", svc.Provider.Description())
	return nil
}
