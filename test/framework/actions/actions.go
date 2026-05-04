// Package actions provides built-in StepFunc implementations for AIVM
// integration test scenarios.
package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	fw "github.com/sisimomo/aivm/test/framework"
)

// RunFunc wraps a plain function as a StepFunc. Use it for inline side-effects
// that don't need access to the harness (e.g., modifying host env vars).
func RunFunc(fn func() error) fw.StepFunc {
	return func(_ context.Context, _ *fw.Harness) error {
		return fn()
	}
}

// CLI invokes an aivm command through the real Cobra CLI entry point, identical
// to how a user runs the tool from a terminal. Use this in preference to the
// individual Do* actions when you want to test flag parsing, cobra routing, or
// the full execution path from entry point to infrastructure.
//
// Examples:
//
//	actions.CLI("start")
//	actions.CLI("bootstrap", "--force")
//	actions.CLI("bootstrap", "--plugin", "java")
//	actions.CLI("rebuild-image", "--force")
func CLI(args ...string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.RunCLI(ctx, args...)
	}
}

// ChangeProvider switches the active AI agent provider. It updates both the
// app config and the active provider reference so subsequent calls (e.g. Start)
// use the new provider.
func ChangeProvider(name string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		prov, ok := h.App.Lifecycle.Agents.Get(name)
		if !ok {
			return fmt.Errorf("provider %q not registered", name)
		}
		h.App.Lifecycle.Config.Agents.Enabled = name
		h.App.Lifecycle.Provider = prov
		return nil
	}
}

// ChangePlugins replaces the list of enabled plugins in the app config.
// The change takes effect on the next DoStart call.
func ChangePlugins(plugins ...string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.App.Lifecycle.Config.Plugins.Enabled = plugins
		return nil
	}
}

// SetVMCreatedDaysAgo backdates the vm-created-at state file so the CLI thinks
// the VM is <days> days old. Use together with WithMaxAgeDays to exercise the
// "VM too old" interactive prompt.
func SetVMCreatedDaysAgo(days int) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		ts := time.Now().AddDate(0, 0, -days).Unix()
		path := filepath.Join(h.StateDir, "vm-created-at")
		return os.WriteFile(path, []byte(strconv.FormatInt(ts, 10)), 0644)
	}
}

// SetBaseImageDaysAgo backdates the base-image.json CreatedAt field so the CLI
// thinks the base image is <days> days old. Use with WithBaseImageMaxAgeDays
// to exercise the "image too old" prompt in DoLaunch.
func SetBaseImageDaysAgo(days int) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		imgPath := filepath.Join(h.StateDir, "base-image.json")
		data, err := os.ReadFile(imgPath)
		if err != nil {
			return fmt.Errorf("base-image.json not found (run Start first): %w", err)
		}
		var img struct {
			ID           string    `json:"id"`
			SnapshotName string    `json:"snapshot_name"`
			CreatedAt    time.Time `json:"created_at"`
		}
		if err := json.Unmarshal(data, &img); err != nil {
			return fmt.Errorf("parse base-image.json: %w", err)
		}
		img.CreatedAt = time.Now().AddDate(0, 0, -days).UTC()
		out, err := json.MarshalIndent(img, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(imgPath, out, 0644)
	}
}

// CreateFakeSession writes a session lock file for a spawned child process.
// The child process (sleep 300) is alive, so session.Store.List() and
// session.Store.CountActive() report this as an active session.
// KillAll() sends SIGTERM to the child, not to the test process, so the
// test binary stays alive after force-rebuild scenarios.
func CreateFakeSession() fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		sessDir := filepath.Join(h.StateDir, "sessions")
		if err := os.MkdirAll(sessDir, 0755); err != nil {
			return err
		}
		// Use a real child process so KillAll() kills the child rather than the
		// test binary itself.
		cmd := exec.Command("sleep", "300")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start fake session process: %w", err)
		}
		pid := cmd.Process.Pid
		lockFile := filepath.Join(sessDir, fmt.Sprintf("%d.lock", pid))
		content := fmt.Sprintf("%d %d\n%s\n", pid, time.Now().Unix(), h.StateDir)
		if err := os.WriteFile(lockFile, []byte(content), 0644); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
		return nil
	}
}

// RemoveFakeSessions removes all *.lock files from the sessions directory,
// clearing any fake sessions created by CreateFakeSession.
func RemoveFakeSessions() fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		sessDir := filepath.Join(h.StateDir, "sessions")
		entries, err := os.ReadDir(sessDir)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".lock" {
				_ = os.Remove(filepath.Join(sessDir, e.Name()))
			}
		}
		return nil
	}
}

// CorruptBootstrapVersion overwrites the "version" field in bootstrap-state.json
// with a stale value. On the next DoStart, the version mismatch triggers a full
// re-bootstrap instead of the incremental sync path.
func CorruptBootstrapVersion() fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		path := filepath.Join(h.StateDir, "bootstrap-state.json")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("bootstrap-state.json not found (run Start first): %w", err)
		}
		var state map[string]interface{}
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("parse bootstrap-state.json: %w", err)
		}
		state["version"] = "old-incompatible-version"
		out, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(path, out, 0644)
	}
}

// ResetOutput clears the harness output buffer. Use between RunCLI calls when
// you want to assert on only a specific command's output.
func ResetOutput() fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.Output.Reset()
		return nil
	}
}

// AddPlugin appends a plugin name to the enabled plugins list in the app config.
// The new plugin will be picked up on the next start or bootstrap call.
func AddPlugin(name string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.App.Lifecycle.Config.Plugins.Enabled = append(
			h.App.Lifecycle.Config.Plugins.Enabled, name,
		)
		return nil
	}
}

// ChangeVMEnv replaces the vm.env map in the app config.
// The change takes effect on the next DoStart call via the envChangedStep.
func ChangeVMEnv(env map[string]string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.App.Lifecycle.Config.VM.Env = env
		return nil
	}
}

// ResetMockVMRunCount resets the primary VM's run counter to zero.
// Use this before a step where you want to assert on the number of scripts
// run by a specific bootstrap phase. No-op if the VM does not implement RunCounter.
func ResetMockVMRunCount() fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		if rc, ok := h.App.Lifecycle.VM.(fw.RunCounter); ok {
			rc.ResetRunCount()
		}
		return nil
	}
}

func RunInVM(script string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.Lifecycle.VM.Run(ctx, script, nil)
	}
}

// RunInVMWithEnv executes a shell script inside the VM with the given
// environment variables set.
func RunInVMWithEnv(script string, env map[string]string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.App.Lifecycle.VM.Run(ctx, script, env)
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
		return h.App.Lifecycle.VM.CreateSnapshot(ctx, name)
	}
}

// RestoreSnapshot restores the VM to a named snapshot.
// Fails if the snapshot does not exist or the restore fails.
func RestoreSnapshot(name string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		found, err := h.App.Lifecycle.VM.RestoreSnapshot(ctx, name)
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
		return h.App.Lifecycle.VM.Start(ctx, opts)
	}
}
