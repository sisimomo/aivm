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

const sleeperCompose = `
services:
  sleeper:
    image: alpine
    command: ["sleep", "3600"]
    restart: "no"
`

// TestComposeLifecycle verifies that compose services configured in aivm.yaml:
//
//  1. Start alongside the VM when `aivm start` is run.
//  2. Appear in `aivm status` output.
//  3. Can be followed via `aivm logs` without immediately erroring.
//  4. Are stopped when `aivm stop` is run.
func TestComposeLifecycle(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithComposeContent(sleeperCompose))

	containerName := h.Profile + "-sleeper-1"

	cancelLogs, bgLogs := actions.AsyncCLI("logs")

	h.Scenario("compose services start, appear in status, logs accessible, stop with VM").
		Step("Start VM (compose services should start alongside)", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Bootstrap complete", assertions.BootstrapComplete()).
		Assert("Compose sleeper container is running", assertContainerRunning(containerName)).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Run: aivm status", actions.CLI("status")).
		Assert("Status output shows compose service name", assertions.OutputContains("sleeper:")).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stream aivm logs in background", bgLogs).
		Step("Wait for logs command to start", sleepStep(1*time.Second)).
		Step("Cancel logs stream", cancelLogs).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stop VM", actions.CLI("stop")).
		Wait("VM is stopped", conditions.VMStatus(vm.StatusStopped), 2*time.Minute).
		Assert("Compose container is gone after stop", assertContainerGone(containerName)).
		Run()
}

// TestComposeLogsAivm verifies that `aivm logs` tails the main aivm log file
// after the VM has started.
func TestComposeLogsAivm(t *testing.T) {
	t.Parallel()
	h := framework.New(t, framework.WithComposeContent(sleeperCompose))

	cancelLogs, bgLogs := actions.AsyncCLI("logs")

	h.Scenario("aivm logs tails the main log file after start").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Step("Reset output buffer", actions.ResetOutput()).
		Step("Stream aivm logs in background", bgLogs).
		Step("Wait for logs to appear", sleepStep(1*time.Second)).
		Step("Cancel logs stream", cancelLogs).
		Assert("Output mentions aivm log activity", assertions.OutputContains("INFO")).
		Run()
}

func assertContainerRunning(containerName string) framework.AssertFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		out, err := exec.CommandContext(ctx, "docker", "inspect",
			"--format", "{{.State.Running}}", containerName).Output()
		if err != nil {
			return fmt.Errorf("container %q: inspect failed: %w", containerName, err)
		}
		if strings.TrimSpace(string(out)) != "true" {
			return fmt.Errorf("container %q is not running (got %q)", containerName, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

func assertContainerGone(containerName string) framework.AssertFunc {
	return func(ctx context.Context, _ *framework.Harness) error {
		cmd := exec.CommandContext(ctx, "docker", "inspect",
			"--format", "{{.State.Running}}", containerName)
		out, err := cmd.CombinedOutput()
		if err != nil {
			outStr := strings.ToLower(string(out))
			if strings.Contains(outStr, "no such object") || strings.Contains(outStr, "no such container") {
				return nil
			}
			return fmt.Errorf("docker inspect %q failed unexpectedly: %w\n%s", containerName, err, string(out))
		}
		if strings.TrimSpace(string(out)) == "true" {
			return fmt.Errorf("container %q is still running after stop", containerName)
		}
		return nil
	}
}
