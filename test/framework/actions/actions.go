// Package actions provides built-in StepFunc implementations for AIVM
// integration test scenarios.
package actions

import (
	"context"
	"fmt"

	"aivm/internal/cli"
	"aivm/internal/vm"
	fw "aivm/test/framework"
)

// Start calls cli.DoStart on the test App, which creates or resumes the VM,
// runs bootstrap (if needed), and saves a base image on first creation.
func Start() fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return cli.DoStart(ctx, h.App)
	}
}

// Stop calls cli.DoStop, which stops the VM and halts the monitor.
func Stop() fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return cli.DoStop(ctx, h.App)
	}
}

// Destroy calls cli.DoDestroy, which deletes the VM entirely.
func Destroy() fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return cli.DoDestroy(ctx, h.App)
	}
}

// RunInVM executes a shell script inside the VM and returns an error if the
// script exits non-zero.
func RunInVM(script string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.VM.Run(ctx, script, nil)
	}
}

// RunInVMWithEnv executes a shell script inside the VM with the given
// environment variables set.
func RunInVMWithEnv(script string, env map[string]string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.VM.Run(ctx, script, env)
	}
}

// StartMonitor launches the idle monitor as an in-process goroutine.
// The monitor is automatically cancelled when the test context expires.
// This is required for scenarios that test idle-based lifecycle transitions.
//
// If cancelDest is non-nil, the cancel function is stored there so callers
// can stop the monitor early.
func StartMonitor(cancelDest *context.CancelFunc) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		cancel := h.RunMonitorInProcess(ctx)
		if cancelDest != nil {
			*cancelDest = cancel
		}
		return nil
	}
}

// CreateSnapshot takes a named snapshot of the current VM state.
func CreateSnapshot(name string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.VM.CreateSnapshot(ctx, name)
	}
}

// RestoreSnapshot restores the VM to a named snapshot.
// Fails if the snapshot does not exist or the restore fails.
func RestoreSnapshot(name string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		found, err := h.App.VM.RestoreSnapshot(ctx, name)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("snapshot %q not found", name)
		}
		return nil
	}
}

// StartWithOptions starts the VM directly (bypassing cli.DoStart bootstrap
// logic) using the given StartOptions. Useful for low-level lifecycle tests
// where you want precise control over VM creation without bootstrap.
func StartWithOptions(opts vm.StartOptions) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.VM.Start(ctx, opts)
	}
}

