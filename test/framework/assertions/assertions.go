// Package assertions provides AssertFunc implementations for AIVM
// e2e test Assert steps.
package assertions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	fw "github.com/sisimomo/aivm/test/framework"
)

// VMStatus asserts that the VM is currently in the given status.
func VMStatus(want vm.Status) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		got, err := h.DockerVM.Status(ctx)
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
// the expected substring.
func VMRunOutput(script, contains string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		out, err := h.DockerVM.RunOutput(ctx, script, nil)
		if err != nil {
			return fmt.Errorf("script %q failed: %w", script, err)
		}
		if !strings.Contains(out, contains) {
			return fmt.Errorf("script %q output does not contain %q\ngot: %s", script, contains, out)
		}
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

// SnapshotCount asserts that the VM has exactly n snapshots.
func SnapshotCount(want int) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		snaps, err := h.DockerVM.ListSnapshots(ctx)
		if err != nil {
			return fmt.Errorf("list snapshots: %w", err)
		}
		if len(snaps) != want {
			names := make([]string, len(snaps))
			for i, s := range snaps {
				names[i] = s.Name
			}
			return fmt.Errorf("snapshot count: got %d %v, want %d", len(snaps), names, want)
		}
		return nil
	}
}

// SnapshotAbsent asserts that no snapshot with the given name exists on the VM.
func SnapshotAbsent(name string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		snaps, err := h.DockerVM.ListSnapshots(ctx)
		if err != nil {
			return fmt.Errorf("list snapshots: %w", err)
		}
		for _, s := range snaps {
			if s.Name == name {
				return fmt.Errorf("snapshot %q still exists but should have been pruned", name)
			}
		}
		return nil
	}
}

// SessionCount asserts that exactly n active sessions exist.
func SessionCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got, err := h.Sessions.CountActive()
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

// AgentLaunched asserts that the active agent provider's launch command was
// called at least once inside the VM.
func AgentLaunched() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		if n := h.ProviderLaunchCount(); n == 0 {
			return fmt.Errorf("expected agent provider.Launch to be called, but it was not")
		}
		return nil
	}
}

// AgentLaunchCount asserts that the active agent provider's launch command was
// called exactly n times inside the VM.
func AgentLaunchCount(want int) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.ProviderLaunchCount()
		if got != want {
			return fmt.Errorf("expected %d agent Launch call(s), got %d", want, got)
		}
		return nil
	}
}

// T3CodeLaunched asserts that the T3 Code service was launched by checking that
// the t3code-url state file exists. This file is written by launchT3Code() and
// removed by Stop().
func T3CodeLaunched() fw.AssertFunc {
	return StateFileExists("t3code-url")
}

// VMFileExists asserts that a file exists inside the VM at the given absolute path.
func VMFileExists(path string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		if err := h.DockerVM.Run(ctx, "test -f "+path, nil); err != nil {
			return fmt.Errorf("expected file %q to exist in VM: %w", path, err)
		}
		return nil
	}
}

// VMFileAbsent asserts that a file does NOT exist inside the VM at the given path.
func VMFileAbsent(path string) fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		if err := h.DockerVM.Run(ctx, "test ! -f "+path, nil); err != nil {
			return fmt.Errorf("expected file %q to be absent in VM: %w", path, err)
		}
		return nil
	}
}

// HostFileExists asserts that a file exists on the host at the given absolute path.
func HostFileExists(path string) fw.AssertFunc {
	return func(_ context.Context, _ *fw.Harness) error {
		fi, err := os.Stat(path)
		if os.IsNotExist(err) {
			return fmt.Errorf("expected host file to exist: %s", path)
		} else if err != nil {
			return fmt.Errorf("stat host file %s: %w", path, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("expected host file to exist but found directory: %s", path)
		}
		return nil
	}
}

// HostFileAbsent asserts that a file does NOT exist on the host at the given path.
func HostFileAbsent(path string) fw.AssertFunc {
	return func(_ context.Context, _ *fw.Harness) error {
		fi, err := os.Stat(path)
		if err == nil {
			if fi.Mode().IsRegular() {
				return fmt.Errorf("expected host file to be absent, but found: %s", path)
			}
			// Path exists but is not a regular file (e.g., directory) - not a failure
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat host file %s: %w", path, err)
		}
		return nil
	}
}

// HostFileContains asserts that the file at path on the host contains needle.
func HostFileContains(path, needle string) fw.AssertFunc {
	return func(_ context.Context, _ *fw.Harness) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read host file %s: %w", path, err)
		}
		if !strings.Contains(string(data), needle) {
			return fmt.Errorf("host file %s does not contain %q\ngot: %s", path, needle, string(data))
		}
		return nil
	}
}

// BootstrapEnvHashSet asserts that the bootstrap state records a non-empty
// env_hash, indicating that vm.env was applied and tracked.
func BootstrapEnvHashSet() fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		state, err := loadBootstrapState(h.StateDir)
		if err != nil {
			return err
		}
		if state == nil {
			return fmt.Errorf("no bootstrap state found")
		}
		if state.EnvHash == "" {
			return fmt.Errorf("expected bootstrap state to record a non-empty env_hash")
		}
		return nil
	}
}

type bootstrapState struct {
	Version    string `json:"version"`
	Provider   string `json:"provider"`
	ConfigHash string `json:"config_hash"`
	EnvHash    string `json:"env_hash"`
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

// T3CodePortAccessible asserts that the T3 Code HTTP server is actually
// reachable on localhost at the port assigned to this harness. Unlike
// T3CodeLaunched (which only checks the state file), this assertion makes a
// real TCP/HTTP connection to verify end-to-end port accessibility.
//
// The assertion retries for up to 15s to allow the in-VM process time to bind
// the port after the Docker container finishes starting.
func T3CodePortAccessible() fw.AssertFunc {
	return func(ctx context.Context, h *fw.Harness) error {
		const totalTimeout = 15 * time.Second
		const retryInterval = 500 * time.Millisecond

		port := h.T3CodePort()
		if port == 0 {
			return fmt.Errorf("T3 Code port is not set — harness not configured with WithT3Code")
		}

		url := fmt.Sprintf("http://localhost:%d/", port)
		client := &http.Client{Timeout: 5 * time.Second}
		deadlineCtx, cancel := context.WithTimeout(ctx, totalTimeout)
		defer cancel()

		var lastErr error
		for {
			req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("build HTTP request: %w", err)
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				return nil // any HTTP response means the port is reachable
			}
			lastErr = err

			select {
			case <-deadlineCtx.Done():
				return fmt.Errorf("T3 Code server not reachable at %s after %s: %w", url, totalTimeout, lastErr)
			case <-time.After(retryInterval):
			}
		}
	}
}
