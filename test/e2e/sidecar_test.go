package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

// TestSidecarLifecycle verifies that a sidecar configured in aivm.yaml:
//
//  1. Starts alongside the VM when `aivm start` is run.
//  2. Appears in `aivm status` output.
//  3. Can be followed via `aivm logs <name>` without immediately erroring.
//  4. Is stopped and removed when `aivm stop` is run.
func TestSidecarLifecycle(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithSidecars(framework.SidecarEntry{
		Name: "sleeper",
		Args: "alpine sleep 3600",
	}))

	containerName := "aivm-" + h.Profile + "-sleeper"

	cancelLogs, bgLogs := actions.AsyncCLI("logs", "sleeper")

	h.Scenario("sidecar starts, appears in status, logs accessible, stops with VM").
		Step("Start VM (sidecar should start alongside)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Sidecar container is running", assertSidecarRunning(containerName)).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm status", actions.CLI("status")).
		Assert("Status output shows sidecar name", assertions.OutputContains("Sidecar sleeper:")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stream sidecar logs in background", bgLogs).
		Step("Wait for logs command to start", sleepStep(1*time.Second)).
		Step("Cancel logs stream", cancelLogs).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("Sidecar container is gone after stop", assertSidecarGone(containerName)).
		Run()
}

// TestSidecarLogsUnknown verifies that requesting logs for a service name that
// is not in the config returns a clear "not found" error.
func TestSidecarLogsUnknown(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithSidecars(framework.SidecarEntry{
		Name: "sleeper",
		Args: "alpine sleep 3600",
	}))

	h.Scenario("aivm logs with unknown service name returns clear error").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm logs unknown-service (exit error ignored)", cliIgnoreExit("logs", "unknown-service")).
		Assert("Error message mentions sidecar not found", assertions.StderrContains("not found in config")).
		Run()
}

// TestSidecarReconciliation_DisabledStops verifies that when a sidecar is
// disabled in the config between runs, its container is stopped and removed the
// next time the VM is started (pruneUnwanted called inside StartAll).
func TestSidecarReconciliation_DisabledStops(t *testing.T) {
	t.Parallel()
	disabled := false
	h := framework.New(t, framework.WithSidecars(
		framework.SidecarEntry{Name: "alpha", Args: "alpine sleep 3600"},
		framework.SidecarEntry{Name: "beta", Args: "alpine sleep 3600"},
	))

	alphaContainer := "aivm-" + h.Profile + "-alpha"
	betaContainer := "aivm-" + h.Profile + "-beta"

	h.Scenario("disabled sidecar container is pruned on next start").
		Step("Start VM (both sidecars enabled)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("alpha container is running", assertSidecarRunning(alphaContainer)).
		Assert("beta container is running", assertSidecarRunning(betaContainer)).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		// Disable beta in config and restart.
		Step("Disable beta sidecar in config", func(_ context.Context, h *framework.Harness) error {
			h.ChangeSidecars([]framework.SidecarEntry{
				{Name: "alpha", Args: "alpine sleep 3600"},
				{Name: "beta", Args: "alpine sleep 3600", Enabled: &disabled},
			})
			return nil
		}).
		// Manually restart beta container to simulate it being left from a previous run.
		Step("Simulate stale beta container from previous run",
			dockerRunDetached(betaContainer, "alpine", "sleep", "3600")).
		Step("Start VM again (pruneUnwanted should remove beta)", actions.CLI("start")).
		Wait("VM is running again", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("alpha container is running", assertSidecarRunning(alphaContainer)).
		Assert("beta container is gone (pruned)", assertSidecarGone(betaContainer)).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Run()
}

// TestSidecarReconciliation_OrphanRemoved verifies that a container matching
// the "aivm-<profile>-*" prefix but not present in the config at all is
// stopped and removed by pruneUnwanted when the VM is started.
func TestSidecarReconciliation_OrphanRemoved(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithSidecars(framework.SidecarEntry{
		Name: "main", Args: "alpine sleep 3600",
	}))

	orphanContainer := "aivm-" + h.Profile + "-orphan"
	// Register the orphan for cleanup in case the test fails mid-way.
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", orphanContainer).Run() //nolint:errcheck
	})

	h.Scenario("orphaned container (prefix match, not in config) is pruned on start").
		// Create the orphan container before the VM starts.
		Step("Create orphan container manually",
			dockerRunDetached(orphanContainer, "alpine", "sleep", "3600")).
		Assert("Orphan container is running before start", assertSidecarRunning(orphanContainer)).
		Step("Start VM (pruneUnwanted should remove orphan)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("main sidecar is running", assertSidecarRunning("aivm-"+h.Profile+"-main")).
		Assert("Orphan container is gone after start", assertSidecarGone(orphanContainer)).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Run()
}

// ── local helpers ─────────────────────────────────────────────────────────────

// assertSidecarRunning returns an AssertFunc that checks the named Docker
// container exists and its State.Running is true.
func assertSidecarRunning(containerName string) framework.AssertFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		out, err := exec.CommandContext(ctx, "docker", "inspect",
			"--format", "{{.State.Running}}", containerName).Output()
		if err != nil {
			return fmt.Errorf("sidecar container %q: inspect failed: %w", containerName, err)
		}
		if strings.TrimSpace(string(out)) != "true" {
			return fmt.Errorf("sidecar container %q is not running (got %q)", containerName, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

// assertSidecarGone returns an AssertFunc that checks the named Docker container
// is no longer running (either removed or stopped).
func assertSidecarGone(containerName string) framework.AssertFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		cmd := exec.CommandContext(ctx, "docker", "inspect",
			"--format", "{{.State.Running}}", containerName)
		out, err := cmd.CombinedOutput()
		if err != nil {
			// Only consider it success if the error is specifically "no such container".
			outStr := string(out)
			if strings.Contains(outStr, "No such object") || strings.Contains(outStr, "No such container") {
				// Container not found → already removed. OK.
				return nil
			}
			// Other error (daemon down, permission denied, etc.) → fail the test.
			return fmt.Errorf("docker inspect %q failed (not a 'no such' error): %w\n%s", containerName, err, outStr)
		}
		if strings.TrimSpace(string(out)) == "true" {
			return fmt.Errorf("sidecar container %q is still running after stop", containerName)
		}
		return nil
	}
}

// cliIgnoreExit runs a CLI command but does not fail the step on a non-zero
// exit code. The output (including cobra's error line) is still captured to
// h.Output so subsequent Assert steps can inspect it.
func cliIgnoreExit(args ...string) framework.StepFunc {
	return func(ctx context.Context, h *framework.Harness) error {
		_ = h.RunCLI(ctx, args...)
		return nil
	}
}

// dockerRunDetached returns a StepFunc that starts a detached Docker container
// with the given name and image+args on the host Docker daemon.
func dockerRunDetached(containerName string, imageAndArgs ...string) framework.StepFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		args := append([]string{"run", "-d", "--name", containerName}, imageAndArgs...)
		cmd := exec.CommandContext(ctx, "docker", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker run %q: %w\n%s", containerName, err, out)
		}
		return nil
	}
}
