package lifecycle

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

// syncStep represents one decision point in the bootstrap state machine.
type syncStep interface {
	applicable(ss *syncState, svc *LifecycleService) bool
	run(ctx context.Context, ss *syncState, svc *LifecycleService) error
}

type syncState struct {
	state      *BootstrapState
	configHash string
}

// syncPipeline is the ordered list of steps evaluated by syncBootstrap.
// The first applicable step is executed; subsequent steps are skipped.
var syncPipeline = []syncStep{
	&missingOrStaleStep{},
	&envChangedStep{},
	&configChangedStep{},
	&upToDateStep{},
}

// syncBootstrap is the main bootstrap entry point on every aivm invocation.
// It reads the host-side state file (no SSH) and returns immediately when
// nothing has changed, or triggers a full reconcile when config has changed.
func (svc *LifecycleService) syncBootstrap(ctx context.Context) error {
	state, err := loadBootstrapState(svc.Config.StateDir)
	if err != nil {
		svc.log().Warn("could not read bootstrap state, running full bootstrap: %v", err)
	}
	configHash := svc.currentConfigHash()
	ss := &syncState{state: state, configHash: configHash}

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
	return ss.state == nil || ss.state.NeedsMigration()
}

func (s *missingOrStaleStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
	return svc.fullBootstrap(ctx, svc.VM, false)
}

// envChangedStep handles changes to vm.env without recreating the VM.
// It only runs when the main config hash is unchanged — if both env and config
// changed, configChangedStep takes over and fullBootstrap re-applies the env.
type envChangedStep struct{}

func (s *envChangedStep) applicable(ss *syncState, svc *LifecycleService) bool {
	return ss.state != nil &&
		ss.state.ConfigHash == ss.configHash &&
		ss.state.Provider == svc.Provider.Name() &&
		ss.state.EnvHash != svc.currentEnvHash()
}

func (s *envChangedStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
	svc.log().Step("VM env changed — re-applying environment variables")
	if err := applyVMEnv(ctx, svc.VM, svc.Config.VM.ResolvedEnv()); err != nil {
		return err
	}
	state, _ := loadBootstrapState(svc.Config.StateDir)
	if state != nil {
		state.EnvHash = svc.currentEnvHash()
		_ = saveBootstrapState(svc.Config.StateDir, state)
	}
	svc.log().Success("Environment variables updated")
	return nil
}

// configChangedStep handles any config change (provider or hash) since the last
// bootstrap by prompting the user to recreate the VM or continue as-is.
type configChangedStep struct{}

func (s *configChangedStep) applicable(ss *syncState, svc *LifecycleService) bool {
	return ss.state != nil && (ss.state.Provider != svc.Provider.Name() || ss.state.ConfigHash != ss.configHash)
}

func (s *configChangedStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
	return svc.resolveConfigChange(ctx)
}

// upToDateStep is the terminal fallthrough: config hash matches, nothing to do.
type upToDateStep struct{}

func (s *upToDateStep) applicable(ss *syncState, _ *LifecycleService) bool {
	return ss.state != nil
}

func (s *upToDateStep) run(_ context.Context, _ *syncState, svc *LifecycleService) error {
	svc.log().Success("VM is up to date — skipping bootstrap")
	return nil
}

// resolveConfigChange handles any config change by prompting the user to
// recreate the VM or continue without applying the change.
func (svc *LifecycleService) resolveConfigChange(ctx context.Context) error {
	svc.log().Warn("VM '%s' config has changed", svc.VM.Profile())

	if !svc.Confirmer.IsInteractive() {
		return fmt.Errorf("VM %q config has changed; rerun interactively to recreate the VM or continue without applying changes", svc.VM.Profile())
	}

	if !promptConfigChanged(svc.log().Out, svc.Confirmer) {
		svc.log().Success("Continuing without applying config changes")
		return nil
	}

	return svc.recreateVM(ctx)
}

// recreateVM terminates all active sessions, destroys the VM, and recreates
// it with a fresh bootstrap.
func (svc *LifecycleService) recreateVM(ctx context.Context) error {
	// Stop the idle monitor (if running from a previous 'aivm start') so it
	// cannot stop or delete the freshly bootstrapped container.
	svc.Monitor.Stop()

	sessions, _ := svc.Sessions.List()
	if len(sessions) > 0 {
		svc.log().Step("Terminating %d active session(s)", len(sessions))
		for _, sess := range sessions {
			proc, err := os.FindProcess(sess.PID)
			if err == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
			sess.Remove()
		}
	}

	clearBootstrapState(svc.Config.StateDir)

	svc.log().Step("Recreating VM")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	imgMgr := svc.imageManager()
	if _, err := svc.bootstrapFreshVM(ctx, svc.VM, imgMgr); err != nil {
		return err
	}

	svc.Sessions.ClearVMStoppedAt()

	svc.log().Success("VM recreated")
	return nil
}
