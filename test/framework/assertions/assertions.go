// Package assertions provides AssertFunc implementations for AIVM
// integration test Assert steps.
package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sisimomo/aivm/internal/vm"
	fw "github.com/sisimomo/aivm/test/framework"
)

// VMStatus asserts that the VM is currently in the given status.
func VMStatus(want vm.Status) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		got, err := h.App.Lifecycle.VM.Status(ctx)
		if err != nil {
			return fmt.Errorf("get VM status: %w", err)
		}
		if got != want {
			return fmt.Errorf("VM status: got %s, want %s", got, want)
		}
		return nil
	}
}

// BaseImageExists asserts that a base image has been recorded in StateDir.
func BaseImageExists() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		path := filepath.Join(h.StateDir, "base-image.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("base image file not found: %s", path)
		} else if err != nil {
			return err
		}
		return nil
	}
}

// BaseImageHasSnapshot asserts that the saved base image includes a snapshot
// name (i.e. the VM backend supports snapshots).
func BaseImageHasSnapshot() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		img := h.ImageManager().LoadBaseImage()
		if img == nil {
			return fmt.Errorf("no base image recorded")
		}
		if img.SnapshotName == "" {
			return fmt.Errorf("base image has no snapshot (VM backend may not support snapshots)")
		}
		return nil
	}
}

// BootstrapComplete asserts that the bootstrap state file exists in StateDir,
// indicating that all configured plugins completed successfully.
func BootstrapComplete() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		path := filepath.Join(h.StateDir, "bootstrap-state.json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("bootstrap state file not found: %s", path)
		} else if err != nil {
			return err
		}
		return nil
	}
}

// VMImageRefCurrent asserts that the VM was created from the current base image.
func VMImageRefCurrent() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		imgMgr := h.ImageManager()
		img := imgMgr.LoadBaseImage()
		if img == nil {
			return fmt.Errorf("no base image found")
		}
		ref := imgMgr.GetVMImageRef()
		if ref != "" && ref != img.ID {
			return fmt.Errorf("VM image ref %q does not match current base image %q", ref, img.ID)
		}
		return nil
	}
}

// VMImageRefIs asserts that the VM image ref equals the given base image ID.
func VMImageRefIs(imageID string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		ref := h.ImageManager().GetVMImageRef()
		if ref != imageID {
			return fmt.Errorf("VM image ref: got %q, want %q", ref, imageID)
		}
		return nil
	}
}

// StateFileExists asserts that a file at path (relative to StateDir) exists.
func StateFileExists(relPath string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		full := filepath.Join(h.StateDir, relPath)
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return fmt.Errorf("expected state file not found: %s", full)
		} else if err != nil {
			return err
		}
		return nil
	}
}

// StateFileAbsent asserts that a file at path (relative to StateDir) does NOT
// exist.
func StateFileAbsent(relPath string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		full := filepath.Join(h.StateDir, relPath)
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("expected state file to be absent, but found: %s", full)
		} else if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
}

// VMRunOutput asserts that running script in the VM produces output containing
// the expected substring. Uses DockerVM.RunOutput if available; otherwise
// falls back to a plain Run with no output check.
func VMRunOutput(script, contains string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		type outputRunner interface {
			RunOutput(ctx context.Context, script string, env map[string]string) (string, error)
		}
		if or, ok := h.App.Lifecycle.VM.(outputRunner); ok {
			out, err := or.RunOutput(ctx, script, nil)
			if err != nil {
				return fmt.Errorf("script %q failed: %w", script, err)
			}
			if !strings.Contains(out, contains) {
				return fmt.Errorf("script %q output does not contain %q\ngot: %s", script, contains, out)
			}
			return nil
		}
		// Fallback for VMs that don't support output capture.
		return h.App.Lifecycle.VM.Run(ctx, script, nil)
	}
}

// BaseImageIs asserts that the current base image ID equals *want.
// The pointer form allows the expected value to be captured in a previous step.
func BaseImageIs(want *string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		img := h.ImageManager().LoadBaseImage()
		if img == nil {
			return fmt.Errorf("no base image recorded")
		}
		if img.ID != *want {
			return fmt.Errorf("base image ID: got %q, want %q", img.ID, *want)
		}
		return nil
	}
}

// BaseImageIsNot asserts that the current base image ID has changed from *prev.
// The pointer form allows the previous value to be captured in an earlier step.
func BaseImageIsNot(prev *string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		img := h.ImageManager().LoadBaseImage()
		if img == nil {
			return fmt.Errorf("no base image recorded")
		}
		if img.ID == *prev {
			return fmt.Errorf("base image ID did not change from %q after rebuild", *prev)
		}
		return nil
	}
}

// VMRunCountIs asserts that the primary VM's Run() was called exactly n times
// since the last ResetMockVMRunCount. Silently passes if the VM does not
// implement RunCounter (e.g. a real Colima VM).
func VMRunCountIs(n int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		rc, ok := h.App.Lifecycle.VM.(fw.RunCounter)
		if !ok {
			return nil
		}
		got := rc.RunCount()
		if got != n {
			return fmt.Errorf("VM run count: got %d, want %d", got, n)
		}
		return nil
	}
}

// VMRunCountAtLeast asserts that the primary VM's Run() was called at least n
// times since the last ResetMockVMRunCount. Silently passes if the VM does not
// implement RunCounter.
func VMRunCountAtLeast(n int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		rc, ok := h.App.Lifecycle.VM.(fw.RunCounter)
		if !ok {
			return nil
		}
		got := rc.RunCount()
		if got < n {
			return fmt.Errorf("VM run count: got %d, want at least %d", got, n)
		}
		return nil
	}
}

// SessionCount asserts that exactly n active sessions exist.
func SessionCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got, err := h.App.Lifecycle.Sessions.CountActive()
		if err != nil {
			return fmt.Errorf("count sessions: %w", err)
		}
		if got != want {
			return fmt.Errorf("session count: got %d, want %d", got, want)
		}
		return nil
	}
}

// BootstrapStateProviderIs asserts that the bootstrap state records the given
// provider name.
func BootstrapStateProviderIs(provider string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		state, err := loadBootstrapState(h.StateDir)
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("no bootstrap state found")
		}
		if state.Provider != provider {
			return fmt.Errorf("bootstrap state provider: got %q, want %q", state.Provider, provider)
		}
		return nil
	}
}

// AgentLaunched asserts that the active agent provider's Launch method was
// called at least once. Use after running `aivm` bare (or any scenario that
// should culminate in an agent session being started).
func AgentLaunched() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		if n := h.ProviderLaunchCount(); n == 0 {
			return fmt.Errorf("expected agent provider.Launch to be called, but it was not")
		}
		return nil
	}
}

// T3CodeLaunched asserts that T3Code.Launch was called at least once.
// Use after running `aivm launch` with T3 Code enabled.
func T3CodeLaunched() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		if n := h.T3CodeLaunchCount(); n == 0 {
			return fmt.Errorf("expected T3Code.Launch to be called, but it was not")
		}
		return nil
	}
}

// T3CodeLaunchCount asserts that T3Code.Launch was called exactly n times.
func T3CodeLaunchCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.T3CodeLaunchCount()
		if got != want {
			return fmt.Errorf("expected %d T3Code.Launch call(s), got %d", want, got)
		}
		return nil
	}
}

// AgentLaunchCount asserts that the active agent provider's Launch method was
// called exactly n times.
func AgentLaunchCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.ProviderLaunchCount()
		if got != want {
			return fmt.Errorf("expected %d agent Launch call(s), got %d", want, got)
		}
		return nil
	}
}

// VMFileExists asserts that a file exists inside the VM at the given path.
// The path must be absolute (e.g. "/tmp/.aivm_test_integ_rtk_claude").
func VMFileExists(path string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		if err := h.App.Lifecycle.VM.Run(ctx, "test -f "+path, nil); err != nil {
			return fmt.Errorf("expected file %q to exist in VM: %w", path, err)
		}
		return nil
	}
}

// VMFileAbsent asserts that a file does NOT exist inside the VM at the given path.
func VMFileAbsent(path string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		if err := h.App.Lifecycle.VM.Run(ctx, "test ! -f "+path, nil); err != nil {
			return fmt.Errorf("expected file %q to be absent in VM: %w", path, err)
		}
		return nil
	}
}

type bootstrapState struct {
	Version    string `json:"version"`
	Provider   string `json:"provider"`
	ConfigHash string `json:"config_hash"`
}

func loadBootstrapState(stateDir string) (*bootstrapState, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "bootstrap-state.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s bootstrapState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse bootstrap-state.json: %w", err)
	}
	return &s, nil
}
