// Package scenarios contains AIVM integration test scenarios.
//
// Run with:
//
//	go test -v -timeout 5m ./test/scenarios/...
package scenarios

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aivm/internal/vm"
	"aivm/test/framework"
	"aivm/test/framework/actions"
	"aivm/test/framework/assertions"
	"aivm/test/framework/conditions"
)

// TestVMCreationFromBaseImage exercises the full VM creation path:
//
//  1. Start creates a fresh Colima VM and runs bootstrap.
//  2. DoStart saves a base image snapshot on first creation.
//  3. After destroying the VM, a second Start restores from the snapshot
//     instead of re-running bootstrap (fast path).
//
// This confirms that the base image lifecycle works end-to-end.
func TestVMCreationFromBaseImage(t *testing.T) {
	h := framework.New(t)

	var firstImageID string

	h.Scenario("VM creation from base image").
		Step("Start VM (first boot — full bootstrap)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Base image saved", assertions.BaseImageExists()).
		Step("Capture base image ID", func(ctx context.Context, h *framework.Harness) error {
			img := h.ImageManager().LoadBaseImage()
			if img == nil {
				return fmt.Errorf("no base image recorded after first boot")
			}
			firstImageID = img.ID
			t.Logf("base image id: %s, snapshot: %q", img.ID, img.SnapshotName)
			return nil
		}).
		Assert("VM image ref is current", assertions.VMImageRefCurrent()).
		Step("Destroy VM", actions.Destroy()).
		Wait("VM is gone", conditions.VMStatus(vm.StatusNotFound), 2*time.Minute).
		Step("Start VM (second boot — restore from base image)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("VM is running", assertions.VMStatus(vm.StatusRunning)).
		Assert("VM image ref matches saved base image", func(ctx context.Context, h *framework.Harness) error {
			return assertions.VMImageRefIs(firstImageID)(ctx, h)
		}).
		Assert("VM image ref is current", assertions.VMImageRefCurrent()).
		Run()
}
