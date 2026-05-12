package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sisimomo/aivm/test/framework"
)

// captureBaseImageID returns a StepFunc that reads the current base image ID
// into the pointed-to string. Fails if no base image has been saved yet.
func captureBaseImageID(t *testing.T, dest *string) framework.StepFunc {
	return func(_ context.Context, h *framework.Harness) error {
		img := h.ImageManager().LoadBaseImage()
		if img == nil {
			return fmt.Errorf("no base image found")
		}
		*dest = img.ID
		t.Logf("captured base image id: %s (snapshot: %q)", img.ID, img.SnapshotName)
		return nil
	}
}

// sleepStep returns a StepFunc that pauses for the given duration.
// Used to ensure timestamps differ when two base images are saved in rapid
// succession (base image IDs use Unix-second precision).
func sleepStep(d time.Duration) framework.StepFunc {
	return func(_ context.Context, _ *framework.Harness) error {
		time.Sleep(d)
		return nil
	}
}
