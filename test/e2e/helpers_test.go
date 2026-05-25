package e2e

import (
	"context"
	"time"

	"github.com/sisimomo/aivm/test/framework"
)

// sleepStep returns a StepFunc that pauses for the given duration.
func sleepStep(d time.Duration) framework.StepFunc {
	return func(_ context.Context, _ *framework.Harness) error {
		time.Sleep(d)
		return nil
	}
}
