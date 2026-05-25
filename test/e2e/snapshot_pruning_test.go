package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestSnapshotPrunedAfterRebuild verifies that rebuilding the base image deletes
// the previous snapshot so that stale snapshots do not accumulate on disk.
//
//  1. Start the VM — full bootstrap runs, base image v1 snapshot is created.
//  2. Rebuild the base image (--force) — base image v2 snapshot is created.
//  3. Assert exactly one snapshot exists (v2 only — v1 was pruned).
//  4. Assert the v1 snapshot name is absent.
func TestSnapshotPrunedAfterRebuild(t *testing.T) {
	t.Parallel()
	h := framework.New(t)

	var v1SnapshotName string

	h.Scenario("old snapshot pruned after rebuild-image").
		Step("Start VM (first boot — creates base image v1)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v1 saved with snapshot", assertions.BaseImageHasSnapshot()).
		Step("Capture v1 snapshot name", func(_ context.Context, h *framework.Harness) error {
			img := h.ImageManager().LoadBaseImage()
			if img == nil {
				return fmt.Errorf("no base image after first boot")
			}
			v1SnapshotName = img.SnapshotName
			t.Logf("base image v1: id=%s snapshot=%q", img.ID, img.SnapshotName)
			return nil
		}).
		Assert("Exactly one snapshot exists after first boot", assertions.SnapshotCount(1)).
		Step("Wait 1s so rebuild gets a new timestamp", func(_ context.Context, _ *framework.Harness) error {
			time.Sleep(1100 * time.Millisecond)
			return nil
		}).
		Step("Rebuild base image (force)", actions.CLI("rebuild-image", "--force")).
		Wait("VM is running after rebuild", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Base image v2 saved with snapshot", assertions.BaseImageHasSnapshot()).
		Assert("Exactly one snapshot exists after rebuild (old one pruned)", assertions.SnapshotCount(1)).
		Assert("v1 snapshot was deleted", func(ctx context.Context, h *framework.Harness) error {
			return assertions.SnapshotAbsent(v1SnapshotName)(ctx, h)
		}).
		Run()
}
