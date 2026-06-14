package lifecycle

import (
	"context"
	"fmt"

	"github.com/sisimomo/aivm/internal/vm"
)

func (svc *LifecycleService) promoteDockerToRuntimeMounts(ctx context.Context) error {
	if effectiveBackend(svc.Config.VM) != "docker" {
		return nil
	}
	runtimeOpts, err := buildRuntimeStartOptions(svc.VM, svc.Config, svc.AgentDefs)
	if err != nil {
		return fmt.Errorf("building runtime start options: %w", err)
	}
	store, ok := vm.AsBaseImageStore(svc.VM)
	if !ok {
		return fmt.Errorf("docker VM does not support base images")
	}
	if svc.baseImageEnabled() {
		if err := store.SaveBaseImage(ctx, runtimeOpts); err != nil {
			svc.logger().Warn(fmt.Sprintf("save base image failed: %v", err))
		}
		return store.RestoreFromBaseImage(ctx, runtimeOpts)
	}
	dockerVM, ok := svc.VM.(*vm.DockerVM)
	if !ok {
		return store.RestoreFromBaseImage(ctx, runtimeOpts)
	}
	return dockerVM.PromoteWithEphemeralCommit(ctx, runtimeOpts)
}

// PromoteDockerToRuntimeMountsForTest exposes promoteDockerToRuntimeMounts for unit tests.
func PromoteDockerToRuntimeMountsForTest(ctx context.Context, svc *LifecycleService) error {
	return svc.promoteDockerToRuntimeMounts(ctx)
}
