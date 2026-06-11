package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

// fullBootstrap destroys live VM, creates fresh, runs plugin bootstrap, saves base (best effort).
func (svc *LifecycleService) fullBootstrap(ctx context.Context) error {
	svc.Monitor.Stop()
	svc.deleteBaseImage(ctx)
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy VM: %w", err)
	}
	opts := buildStartOptions(svc.VM, svc.Config, svc.AgentDefs)
	ensureAgentPersistDirs(svc.Config, svc.AgentDefs)
	if err := svc.VM.Start(ctx, opts); err != nil {
		return err
	}
	if err := svc.VM.WaitReady(ctx, 60*time.Second); err != nil {
		return err
	}
	vm.RecordVMCreation(svc.Config.StateDir)
	vm.RecordBootstrapAt(svc.Config.StateDir)
	if err := svc.bootstrap(ctx, svc.VM); err != nil {
		return err
	}
	_ = svc.SaveBaseImageBestEffort(ctx, opts)
	if err := svc.Compose.Up(ctx); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}
	return nil
}

// fastRecreate restores from base, reapplies env/git, skips plugins.
func (svc *LifecycleService) fastRecreate(ctx context.Context) error {
	store, ok := vm.AsBaseImageStore(svc.VM)
	if !ok || !svc.hasValidBase(ctx) {
		svc.logger().Warn("No valid base image — falling back to full bootstrap")
		return svc.fullBootstrap(ctx)
	}
	opts := buildStartOptions(svc.VM, svc.Config, svc.AgentDefs)
	ensureAgentPersistDirs(svc.Config, svc.AgentDefs)
	ctx, cancel := context.WithTimeout(ctx, vm.BaseImageOpTimeout)
	defer cancel()
	if err := store.RestoreFromBaseImage(ctx, opts); err != nil {
		svc.logger().Warn(fmt.Sprintf("restore failed, deleting base: %v", err))
		svc.deleteBaseImage(ctx)
		return svc.fullBootstrap(ctx)
	}
	if err := svc.VM.WaitReady(ctx, 60*time.Second); err != nil {
		svc.deleteBaseImage(ctx)
		return svc.fullBootstrap(ctx)
	}
	vm.RecordVMCreation(svc.Config.StateDir) // reset VM age only
	if err := applyPostRestore(ctx, svc); err != nil {
		return err
	}
	if err := svc.Compose.Up(ctx); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}
	return nil
}

func applyPostRestore(ctx context.Context, svc *LifecycleService) error {
	if err := applyVMEnv(ctx, svc.VM, svc.Config.VM.ResolvedEnv()); err != nil {
		return err
	}
	name, email := readHostGitIdentity()
	return applyGitIdentity(ctx, svc.VM, name, email)
}

// ApplyPostRestoreForTest exposes applyPostRestore for unit tests.
func ApplyPostRestoreForTest(ctx context.Context, svc *LifecycleService) error {
	return applyPostRestore(ctx, svc)
}

// FastRecreateForTest exposes fastRecreate for unit tests.
func FastRecreateForTest(ctx context.Context, svc *LifecycleService) error {
	return svc.fastRecreate(ctx)
}
