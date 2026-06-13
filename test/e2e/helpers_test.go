package e2e

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/sisimomo/aivm/test/framework"
)

// skipUnlessDocker skips the test when Docker is unavailable (e.g. CI without
// a daemon). E2E tests require Docker to run the container VM backend.
func skipUnlessDocker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		t.Skipf("docker not available: %v", err)
	}
}

// sleepStep returns a StepFunc that pauses for the given duration.
func sleepStep(d time.Duration) framework.StepFunc {
	return func(_ context.Context, _ *framework.Harness) error {
		time.Sleep(d)
		return nil
	}
}
