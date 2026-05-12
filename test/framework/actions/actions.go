// Package actions provides built-in StepFunc implementations for AIVM
// integration test scenarios.
package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	fw "github.com/sisimomo/aivm/test/framework"
)

// RunFunc wraps a plain function as a StepFunc. Use it for inline side-effects
// that don't need access to the harness (e.g., modifying host env vars).
func RunFunc(fn func() error) fw.StepFunc {
	return func(_ context.Context, _ *fw.Harness) error {
		return fn()
	}
}

// CLI invokes an aivm command through the real aivm-test binary, identical
// to how a user runs the tool from a terminal. This exercises flag parsing,
// cobra routing, and the full execution path from entry point to infrastructure.
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

// AsyncCLI runs an aivm command in a background goroutine and returns
// immediately so the scenario can proceed while the subprocess is live.
// This is used for session idle tests where aivm (bare) must hold a session
// lock file open without blocking the scenario runner.
//
// Usage:
//
//	cancelFn, bgStep := actions.AsyncCLI()
//	scenario.
//	    Step("Launch agent in background", bgStep).
//	    // ... assert session active, VM still running ...
//	    Step("Cancel background session", cancelFn)
//
// The returned cancel StepFunc sends cancellation to the background subprocess
// and waits up to 5 s for it to exit. Always call cancel — it is safe to call
// multiple times. The goroutine is also cancelled automatically when the test
// ends via context propagation.
func AsyncCLI(args ...string) (cancel fw.StepFunc, bg fw.StepFunc) {
	ctx, cancelCtx := context.WithCancel(context.Background())

	done := make(chan struct{})

	bg = func(_ context.Context, h *fw.Harness) error {
		go func() {
			defer close(done)
			_ = h.RunCLI(ctx, args...)
		}()
		return nil
	}

	cancel = func(_ context.Context, _ *fw.Harness) error {
		cancelCtx()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return nil
	}

	return cancel, bg
}

// SetWorkDir permanently overrides the working directory for subsequent RunCLI
// calls. Use this to test CWD-sensitive behaviour (e.g. CWD outside DevRoot).
func SetWorkDir(dir string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.SetWorkDir(dir)
		return nil
	}
}

// ChangeProvider switches the active AI agent provider. It updates the config
// and rewrites aivm.yaml so subsequent CLI calls use the new provider.
func ChangeProvider(name string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.ChangeProvider(name)
		return nil
	}
}

// ChangePlugins replaces the list of enabled plugins in the config.
// The change takes effect on the next CLI call.
func ChangePlugins(plugins ...string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.ChangePlugins(plugins)
		return nil
	}
}

// AddPlugin appends a plugin name to the enabled plugins list in the config.
// The new plugin will be picked up on the next start or bootstrap call.
func AddPlugin(name string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.AppendPlugin(name)
		return nil
	}
}

// ChangeVMEnv replaces the vm.env map in the config.
// The change takes effect on the next CLI call.
func ChangeVMEnv(env map[string]string) fw.StepFunc {
	return func(_ context.Context, h *fw.Harness) error {
		h.ChangeVMEnv(env)
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
// to exercise the "image too old" prompt.
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

// CorruptBootstrapVersion overwrites the "version" field in bootstrap-state.json
// with a stale value. On the next CLI start, the version mismatch triggers a full
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

// RunInVM executes a shell script inside the VM container.
func RunInVM(script string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.DockerVM.Run(ctx, script, nil)
	}
}

// RunInVMWithEnv executes a shell script inside the VM container with the
// given environment variables set.
func RunInVMWithEnv(script string, env map[string]string) fw.StepFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		return h.DockerVM.Run(ctx, script, env)
	}
}
