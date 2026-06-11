package lifecycle

import (
	"context"
	"fmt"

	"github.com/sisimomo/aivm/internal/vm"
)

func (svc *LifecycleService) baseImageEnabled() bool {
	return svc.Config.VM.BaseImageEnable
}

func (svc *LifecycleService) hasValidBase(ctx context.Context) bool {
	if !svc.baseImageEnabled() {
		return false
	}
	store, ok := vm.AsBaseImageStore(svc.VM)
	if !ok {
		return false
	}
	state, _ := loadBootstrapState(svc.Config.StateDir)
	check := BaseImageCheck{
		ConfigHash:     svc.currentConfigHash(),
		Backend:        effectiveBackend(svc.Config.VM),
		VMType:         effectiveVMType(svc.Config.VM),
		ArtifactExists: store.HasBaseImage(ctx),
	}
	return BaseImageValid(state, check)
}

func (svc *LifecycleService) deleteBaseImage(ctx context.Context) {
	store, ok := vm.AsBaseImageStore(svc.VM)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, vm.BaseImageOpTimeout)
	defer cancel()
	if err := store.DeleteBaseImage(ctx); err != nil {
		svc.logger().Warn(fmt.Sprintf("delete base image: %v", err))
	}
}

func (svc *LifecycleService) SaveBaseImageBestEffort(ctx context.Context, opts vm.StartOptions) error {
	if !svc.baseImageEnabled() {
		return nil
	}
	store, ok := vm.AsBaseImageStore(svc.VM)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, vm.BaseImageOpTimeout)
	defer cancel()
	if err := store.SaveBaseImage(ctx, opts); err != nil {
		svc.logger().Warn(fmt.Sprintf("save base image failed (VM still usable): %v", err))
		return err
	}
	svc.logger().Info("Base image saved")
	return nil
}
