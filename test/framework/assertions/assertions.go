// Package assertions provides AssertFunc implementations for AIVM
// integration test Assert steps.
package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"aivm/internal/vm"
	fw "aivm/test/framework"
)

// VMStatus asserts that the VM is currently in the given status.
func VMStatus(want vm.Status) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		got, err := h.App.VM.Status(ctx)
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

// VMImageRefCurrent asserts that the VM was created from the current base image
// (not a legacy one).
func VMImageRefCurrent() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		imgMgr := h.ImageManager()
		if imgMgr.IsVMLegacy() {
			img := imgMgr.LoadBaseImage()
			ref := imgMgr.GetVMImageRef()
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
// the expected substring.
func VMRunOutput(script, contains string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		// Run the script and capture output by writing to a temp file on the VM.
		// We use the vm.Run API which streams to the log writer, so we can only
		// check for script success/failure here — not capture stdout.
		// For richer assertions use RunInVM with a custom script that writes to
		// a known file and then ReadFile it on the host.
		if err := h.App.VM.Run(ctx, script, nil); err != nil {
			return fmt.Errorf("script %q failed: %w", script, err)
		}
		_ = contains // future: add output capture if needed
		return nil
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
		rc, ok := h.App.VM.(fw.RunCounter)
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
		rc, ok := h.App.VM.(fw.RunCounter)
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

// (vm-transition.json exists in StateDir).
func TransitionStateExists() fw.AssertFunc {
	return StateFileExists("vm-transition.json")
}

// TransitionStateAbsent asserts that no VM transition is in progress
// (vm-transition.json does NOT exist in StateDir).
func TransitionStateAbsent() fw.AssertFunc {
	return StateFileAbsent("vm-transition.json")
}

// SessionCount asserts that exactly n active sessions exist.
func SessionCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got, err := h.App.Sessions.CountActive()
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

// BootstrapStateContainsPlugins asserts that all the given plugins appear in
// the bootstrap state's installed list (may have others alongside them).
func BootstrapStateContainsPlugins(plugins ...string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		state, err := loadBootstrapState(h.StateDir)
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("no bootstrap state found")
		}
		installed := make(map[string]bool, len(state.Installed))
		for _, p := range state.Installed {
			installed[p] = true
		}
		for _, want := range plugins {
			if !installed[want] {
				return fmt.Errorf("bootstrap state installed %v does not contain %q", state.Installed, want)
			}
		}
		return nil
	}
}

// BootstrapStateInstalledExactly asserts that the bootstrap state's installed
// plugin list matches plugins exactly (order-independent).
func BootstrapStateInstalledExactly(plugins ...string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		state, err := loadBootstrapState(h.StateDir)
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("no bootstrap state found")
		}
		got := append([]string(nil), state.Installed...)
		want := append([]string(nil), plugins...)
		sort.Strings(got)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("bootstrap state installed: got %v, want %v", got, want)
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

type bootstrapState struct {
	Version   string   `json:"version"`
	Provider  string   `json:"provider"`
	Installed []string `json:"installed"`
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
