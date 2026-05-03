// Package conditions provides ConditionFunc implementations for AIVM
// integration test Wait steps.
package conditions

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sisimomo/aivm/internal/vm"
	fw "github.com/sisimomo/aivm/test/framework"
)

// VMStatus returns a ConditionFunc that resolves to true when the VM reaches
// the given status.
func VMStatus(want vm.Status) fw.ConditionFunc {
	return func(ctx context.Context, h *fw.Harness) (bool, error) {
		got, err := h.App.Lifecycle.VM.Status(ctx)
		if err != nil {
			return false, err
		}
		return got == want, nil
	}
}

// FileExists returns a ConditionFunc that resolves to true when the file at
// the given absolute host path exists.
func FileExists(path string) fw.ConditionFunc {
	return func(_ context.Context, _ *fw.Harness) (bool, error) {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil, err
	}
}

// StateFileExists returns a ConditionFunc that resolves to true when the file
// at path (relative to the harness StateDir) exists.
func StateFileExists(relPath string) fw.ConditionFunc {
	return func(_ context.Context, h *fw.Harness) (bool, error) {
		_, err := os.Stat(filepath.Join(h.StateDir, relPath))
		if os.IsNotExist(err) {
			return false, nil
		}
		return err == nil, err
	}
}

// StateFileAbsent returns a ConditionFunc that resolves to true when the file
// at path (relative to the harness StateDir) does NOT exist.
func StateFileAbsent(relPath string) fw.ConditionFunc {
	return func(_ context.Context, h *fw.Harness) (bool, error) {
		_, err := os.Stat(filepath.Join(h.StateDir, relPath))
		if os.IsNotExist(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}
}

// BaseImageExists returns a ConditionFunc that resolves to true when a base
// image has been saved in the harness StateDir.
func BaseImageExists() fw.ConditionFunc {
	return StateFileExists("base-image.json")
}

// BootstrapComplete returns a ConditionFunc that resolves to true when the
// bootstrap state file exists in the harness StateDir.
func BootstrapComplete() fw.ConditionFunc {
	return StateFileExists("bootstrap-state.json")
}

// TransitionStateExists returns a ConditionFunc that resolves to true when a
// VM transition is in progress (vm-transition.json exists).
func TransitionStateExists() fw.ConditionFunc {
	return StateFileExists("vm-transition.json")
}

// TransitionStateAbsent returns a ConditionFunc that resolves to true when no
// VM transition is in progress (vm-transition.json does not exist).
func TransitionStateAbsent() fw.ConditionFunc {
	return StateFileAbsent("vm-transition.json")
}

// SessionCount returns a ConditionFunc that resolves to true when exactly n
// active sessions exist.
func SessionCount(want int) fw.ConditionFunc {
	return func(_ context.Context, h *fw.Harness) (bool, error) {
		got, err := h.App.Lifecycle.Sessions.CountActive()
		if err != nil {
			return false, err
		}
		return got == want, nil
	}
}
