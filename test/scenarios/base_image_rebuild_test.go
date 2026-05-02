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

// TestBaseImageRebuildImpact verifies the base image rebuild flow:
//
//  1. Start the VM — full bootstrap runs, base image v1 is saved.
//  2. Destroy the VM.
//  3. Start again — VM is restored from base image v1 (no bootstrap).
//  4. Trigger a hard rebuild via DoRebuildImage — new VM, bootstrap, base image v2.
//  5. Confirm the new VM uses base image v2.
//
// This mirrors the real rebuild-image command with force=true (non-interactive).
func TestBaseImageRebuildImpact(t *testing.T) {
	h := framework.New(t)

	var v1ID, v2ID string

	h.Scenario("base image rebuild impact").
		Step("Start VM (first boot — creates base image v1)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved", assertions.BaseImageExists()).
		Step("Capture base image v1 ID", func(_ context.Context, h *framework.Harness) error {
			img := h.ImageManager().LoadBaseImage()
			if img == nil {
				return fmt.Errorf("no base image after first boot")
			}
			v1ID = img.ID
			t.Logf("base image v1: id=%s snapshot=%q", img.ID, img.SnapshotName)
			return nil
		}).
		Step("Destroy VM", actions.Destroy()).
		Wait("VM is gone", conditions.VMStatus(vm.StatusNotFound), 2*time.Minute).
		Step("Start VM (restores from base image v1)", actions.Start()).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 3*time.Minute).
		Assert("VM image ref is v1", func(_ context.Context, h *framework.Harness) error {
			return assertions.VMImageRefIs(v1ID)(context.Background(), h)
		}).
		Step("Wait 1s so rebuild gets a new timestamp", func(_ context.Context, _ *framework.Harness) error {
			time.Sleep(1100 * time.Millisecond)
			return nil
		}).
		Step("Rebuild base image (force — no interactive prompt)", func(ctx context.Context, h *framework.Harness) error {
			return rebuildForceNoSessions(ctx, h)
		}).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v2 exists", assertions.BaseImageExists()).
		Step("Capture base image v2 ID", func(_ context.Context, h *framework.Harness) error {
			img := h.ImageManager().LoadBaseImage()
			if img == nil {
				return fmt.Errorf("no base image after rebuild")
			}
			v2ID = img.ID
			t.Logf("base image v2: id=%s snapshot=%q", img.ID, img.SnapshotName)
			if v2ID == v1ID {
				return fmt.Errorf("base image ID did not change after rebuild: %s", v1ID)
			}
			return nil
		}).
		Assert("VM image ref is current (v2)", assertions.VMImageRefCurrent()).
		Run()
}

// rebuildForceNoSessions runs the hard-rebuild path directly, bypassing the
// interactive prompts. It destroys the current VM, starts a new one, runs
// bootstrap, and saves a new base image.
func rebuildForceNoSessions(ctx context.Context, h *framework.Harness) error {
	imgMgr := h.ImageManager()

	if err := h.App.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroy VM: %w", err)
	}

	opts := vm.StartOptions{
		CPUs:      h.App.Config.VM.CPUs,
		MemoryGiB: h.App.Config.VM.MemoryGiB,
		DiskGiB:   h.App.Config.VM.DiskGiB,
		VMType:    h.App.Config.VM.Type,
	}
	if err := h.App.VM.Start(ctx, opts); err != nil {
		return fmt.Errorf("start new VM: %w", err)
	}
	if err := h.App.VM.WaitReady(ctx, 3*time.Minute); err != nil {
		return fmt.Errorf("wait VM ready: %w", err)
	}

	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("save base image: %w", err)
	}
	imgMgr.RecordVMImageRef(img.ID)
	vm.ClearTransitionState(h.StateDir)
	return nil
}
