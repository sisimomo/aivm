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
&providerMismatchStep{},
&hashChangedStep{},
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

// providerMismatchStep handles the case where the configured agent has changed
// since the last bootstrap — prompts the user to install the new agent or recreate.
type providerMismatchStep struct{}

func (s *providerMismatchStep) applicable(ss *syncState, svc *LifecycleService) bool {
return ss.state != nil && ss.state.Provider != svc.Provider.Name()
}

func (s *providerMismatchStep) run(ctx context.Context, ss *syncState, svc *LifecycleService) error {
return svc.resolveAgentMismatch(ctx, ss.state)
}

// hashChangedStep runs a full bootstrap when the config hash has changed,
// meaning plugins, integrations, or provider config was modified.
type hashChangedStep struct{}

func (s *hashChangedStep) applicable(ss *syncState, _ *LifecycleService) bool {
return ss.state != nil && ss.state.ConfigHash != ss.configHash
}

func (s *hashChangedStep) run(ctx context.Context, _ *syncState, svc *LifecycleService) error {
return svc.fullBootstrap(ctx, svc.VM, false)
}

// upToDateStep is the terminal fallthrough: config hash matches, nothing to do.
type upToDateStep struct{}

func (s *upToDateStep) applicable(ss *syncState, _ *LifecycleService) bool {
return ss.state != nil
}

func (s *upToDateStep) run(_ context.Context, _ *syncState, svc *LifecycleService) error {
svc.log().Info("VM is up to date — skipping bootstrap")
return nil
}

// resolveAgentMismatch handles the case where the VM has a different agent than
// the configured one. Prompts the user to install the new agent or recreate the VM.
func (svc *LifecycleService) resolveAgentMismatch(ctx context.Context, state *BootstrapState) error {
old, ok := svc.Agents.Get(state.Provider)
var oldDesc string
if ok {
oldDesc = old.Description()
} else {
oldDesc = state.Provider
}
configured := svc.Provider.Description()

svc.log().Warn("VM '%s' was created for a different agent", svc.VM.Profile())
svc.log().Warn("Installed agent: %s", oldDesc)
svc.log().Warn("Configured agent: %s", configured)

if !svc.Confirmer.IsInteractive() {
return fmt.Errorf(
"VM %q was created for %s, but config selects %s; rerun interactively to choose whether to install %s into the existing VM or recreate it with only %s",
svc.VM.Profile(),
oldDesc,
configured,
configured,
configured,
)
}

sessions, _ := svc.Sessions.List()
decision, ok := promptAgentMismatch(svc.log().Out, svc.Confirmer, oldDesc, configured, len(sessions))
if !ok {
return fmt.Errorf("invalid choice")
}

switch decision {
case agentMismatchInstall:
return svc.fullBootstrap(ctx, svc.VM, false)
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

svc.log().Step("Recreating VM for %s", svc.Provider.Description())
if err := svc.VM.Destroy(ctx); err != nil {
return fmt.Errorf("destroying VM: %w", err)
}

imgMgr := svc.imageManager()
if _, err := svc.bootstrapFreshVM(ctx, svc.VM, imgMgr); err != nil {
return err
}

svc.Sessions.ClearVMStoppedAt()

svc.log().Success("VM recreated with only %s", svc.Provider.Description())
return nil
}
