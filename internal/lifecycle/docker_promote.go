package lifecycle

import (
	"context"
	"fmt"
)

func (svc *LifecycleService) promoteDockerToRuntimeMounts(ctx context.Context) error {
	if effectiveBackend(svc.Config.VM) != "docker" {
		return nil
	}
	runtimeOpts, err := buildRuntimeStartOptions(svc.VM, svc.Config, svc.AgentDefs)
	if err != nil {
		return fmt.Errorf("building runtime start options: %w", err)
	}
	return svc.VM.FinalizeAfterBootstrap(ctx, runtimeOpts)
}

// PromoteDockerToRuntimeMountsForTest exposes promoteDockerToRuntimeMounts for unit tests.
func PromoteDockerToRuntimeMountsForTest(ctx context.Context, svc *LifecycleService) error {
	return svc.promoteDockerToRuntimeMounts(ctx)
}
