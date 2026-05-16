// Package framework provides the e2e testing harness for AIVM.
// It creates isolated Docker-container VM environments per test, invokes the
// real aivm-test binary as a subprocess (identical to real user invocations),
// and tears everything down on completion.
//
// Each test gets a unique profile name and a temp state directory at
// ~/.aivm/test-runs/<profile>/state, ensuring complete isolation.
//
// Usage:
//
//	func TestMyScenario(t *testing.T) {
//	    h := framework.New(t, framework.WithCPUs(2))
//	    h.Scenario("my scenario").
//	        Step("Start VM", actions.Start()).
//	        Wait("VM running", conditions.VMStatus(vm.StatusRunning), 90*time.Second).
//	        Assert("Bootstrap complete", assertions.BootstrapComplete()).
//	        Run()
//	}
package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/vm"
)

// Harness holds the full isolated test environment for one test.
// Each Harness gets a unique Docker container profile and temp state directory.
// Both are always cleaned up when the test finishes, even on failure.
type Harness struct {
	t        *testing.T
	tc       testConfig
	StateDir string
	Profile  string
	// DockerVM gives direct container access for assertions that read container
	// state (VMStatus, VMFileExists, VMRunOutput, etc.).
	DockerVM *vm.DockerVM
	// Sessions allows counting active session lock files.
	Sessions *session.Store
	// Output captures all stdout/stderr written by the subprocess.
	// Use Output.Stdout() / Output.Stderr() in assertions, and Output.Reset()
	// between RunCLI calls when per-command isolation matters.
	Output         *OutputBuffer
	workDir        string
	composeStarted bool
}

// New creates a new Harness for the calling test.
// The Harness is fully wired (aivm-test subprocess, Docker container VM) and
// registers a t.Cleanup that stops all containers and removes the temp state
// directory.
//
// Requires the aivm-test-base:latest Docker image to be present. Build it once
// with: docker build -t aivm-test-base:latest ./test/docker/
// and aivm-test to be on PATH: make install-test
func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()

	// Ensure the base Docker image exists, building it if needed.
	if err := BuildTestImage(); err != nil {
		t.Fatalf("harness: ensure test image: %v", err)
	}

	tc := defaultTestConfig()
	for _, opt := range opts {
		opt(&tc)
	}

	suffix := mustRandomHex(6)
	profile := "aivm-test-" + suffix

	// Use ~/.aivm/test-runs/ so the path is stable and easy to find.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("harness: get home dir: %v", err)
	}
	testRunDir := filepath.Join(home, ".aivm", "test-runs", profile)
	stateDir := filepath.Join(testRunDir, "state")

	if tc.DevRoot == "" {
		tc.DevRoot = filepath.Join(testRunDir, "dev")
	}

	for _, dir := range []string{
		stateDir,
		filepath.Join(stateDir, "logs"),
		filepath.Join(stateDir, "sessions"),
		tc.DevRoot,
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("harness: create dir %s: %v", dir, err)
		}
	}

	dockerVM := vm.NewDocker(profile, stateDir, TestImageName)
	sessions := session.NewStore(stateDir)

	h := &Harness{
		t:        t,
		tc:       tc,
		StateDir: stateDir,
		Profile:  profile,
		DockerVM: dockerVM,
		Sessions: sessions,
		Output:   &OutputBuffer{},
		workDir:  tc.DevRoot,
	}

	// Write initial config YAML (and compose file if configured).
	h.WriteConfig()

	t.Cleanup(func() {
		h.killIdleMonitor()
		// Tear down any compose services created for this profile. These are
		// normally stopped by 'aivm stop/destroy', but may be left running if
		// the test fails before reaching that step.
		if h.composeStarted {
			composeFile := filepath.Join(h.StateDir, "docker-compose.yml")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cmd := exec.CommandContext(ctx, "docker", "compose",
				"-f", composeFile,
				"--project-name", h.Profile,
				"down", "-v")
			_ = cmd.Run()
			cancel()
		}
		dockerVM.DestroyWithImages()
		// Files inside the .t3 bind-mount may be owned by the container user
		// (different UID than the host test runner). Use sudo chmod -R 0777 so
		// that subdirectories created after bootstrap (e.g. .t3/caches/) are
		// also made world-writable and os.RemoveAll can delete them.
		chmodCtx, chmodCancel := context.WithTimeout(context.Background(), 5*time.Second)
		chmodCmd := exec.CommandContext(chmodCtx, "sudo", "-n", "chmod", "-R", "0777", testRunDir)
		if err := chmodCmd.Run(); err != nil {
			chmodCancel()
			// Fallback: best-effort Go walk (works when host user owns the files).
			_ = chmodAllWritable(testRunDir)
		} else {
			chmodCancel()
		}
		if err := os.RemoveAll(testRunDir); err != nil {
			t.Logf("harness cleanup: remove test run dir %q: %v", testRunDir, err)
		}
	})

	return h
}

// WriteConfig writes the current testConfig as aivm.yaml in the state directory,
// and writes docker-compose.yml when ComposeContent is set.
// Called automatically by New() and by all mutator methods (ChangeProvider,
// ChangePlugins, ChangeVMEnv, AppendPlugin) after updating tc.
func (h *Harness) WriteConfig() {
	h.t.Helper()
	if h.tc.ComposeContent != "" {
		composePath := filepath.Join(h.StateDir, "docker-compose.yml")
		if err := os.WriteFile(composePath, []byte(h.tc.ComposeContent), 0644); err != nil {
			h.t.Fatalf("harness: write docker-compose.yml: %v", err)
		}
		h.composeStarted = true
	}
	yaml := buildTestYAML(h.Profile, h.StateDir, h.tc)
	path := filepath.Join(h.StateDir, "aivm.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		h.t.Fatalf("harness: write aivm.yaml: %v", err)
	}
}

// RunCLI executes an aivm CLI command as a subprocess using the installed
// aivm-test binary. This exercises the same code path as a real user invocation
// including flag parsing, cobra routing, and the full execution path.
//
// Output is captured into h.Output (both stdout and stderr).
// The subprocess uses h.StateDir as AIVM_STATE_DIR for isolation.
//
// Example:
//
//	h.RunCLI(ctx, "start")
//	h.RunCLI(ctx, "bootstrap", "--force")
//	h.RunCLI(ctx, "bootstrap", "--plugin", "java")
func (h *Harness) RunCLI(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "aivm-test", args...)
	cmd.Dir = h.workDir
	cmd.Env = h.buildSubprocessEnv()
	cmd.Stdout = h.Output
	cmd.Stderr = &stderrWriter{h.Output}
	// WaitDelay ensures that when the context is cancelled (e.g. by AsyncCLI's
	// cancel step), the I/O copy goroutines are forcibly terminated after 3s
	// even if child processes spawned by aivm-test (e.g. docker exec) still
	// hold the pipe write-ends open. Without this, cmd.Run() blocks until the
	// child processes exit, causing the 5s cancel timeout to fire.
	cmd.WaitDelay = 3 * time.Second

	if h.tc.Interactive {
		stdin := strings.Join(h.tc.StdinAnswers, "\n")
		if !strings.HasSuffix(stdin, "\n") {
			stdin += "\n"
		}
		cmd.Stdin = strings.NewReader(stdin)
	}

	return cmd.Run()
}

// Scenario creates a new Scenario builder attached to this Harness.
func (h *Harness) Scenario(name string) *Scenario {
	return newScenario(name, h)
}

// SetWorkDir permanently overrides the working directory for subsequent RunCLI
// calls. Use this to test CWD-sensitive behaviour (e.g. CWD outside DevRoot).
func (h *Harness) SetWorkDir(dir string) {
	h.workDir = dir
}

// ChangeProvider switches the active AI agent provider. Updates tc and rewrites
// the YAML config so the next RunCLI picks up the change.
func (h *Harness) ChangeProvider(name string) {
	h.tc.Provider = name
	h.WriteConfig()
}

// ChangePlugins replaces the list of enabled plugins in the config. Updates tc
// and rewrites the YAML config so the next RunCLI picks up the change.
func (h *Harness) ChangePlugins(plugins []string) {
	h.tc.Plugins = plugins
	h.WriteConfig()
}

// AppendPlugin appends a plugin name to the enabled plugins list. Updates tc
// and rewrites the YAML config so the next RunCLI picks up the change.
func (h *Harness) AppendPlugin(name string) {
	h.tc.Plugins = append(h.tc.Plugins, name)
	h.WriteConfig()
}

// ChangeVMEnv replaces the vm.env map. Updates tc and rewrites the YAML config
// so the next RunCLI picks up the change.
func (h *Harness) ChangeVMEnv(env map[string]string) {
	h.tc.VMEnv = env
	h.WriteConfig()
}

// ChangeComposeFile replaces the docker-compose.yml content and rewrites both
// the compose file and aivm.yaml so the next RunCLI picks up the change.
func (h *Harness) ChangeComposeFile(content string) {
	h.tc.ComposeContent = content
	if content == "" {
		h.composeStarted = false
	}
	h.WriteConfig()
}

// ProviderLaunchCount returns the number of times the agent launch command was
// executed inside the Docker container. It reads the line count of the
// /tmp/.aivm_agent_launched marker file (each launch appends one line).
// Returns 0 on any error (container not started, file absent, etc.).
func (h *Harness) ProviderLaunchCount() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := h.DockerVM.RunOutput(ctx, "cat /tmp/.aivm_agent_launched 2>/dev/null | wc -l", nil)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0
	}
	return n
}

// ImageManager returns the ImageManager for the test VM, scoped to StateDir.
func (h *Harness) ImageManager() *vm.ImageManager {
	return vm.NewImageManager(h.DockerVM, h.StateDir)
}

// T3CodePort returns the T3 Code port configured for this harness.
// Valid only when the harness was created with WithT3Code.
func (h *Harness) T3CodePort() int {
	return h.tc.T3CodePort
}

// ── internal helpers ───────────────────────────────────────────────────────

// killIdleMonitor reads the idle-monitor.pid from StateDir and sends SIGTERM
// to the daemon process, then waits briefly for it to exit.
func (h *Harness) killIdleMonitor() {
	pidFile := filepath.Join(h.StateDir, "idle-monitor.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return // no pid file — monitor never started or already gone
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}

	// Verify the PID belongs to idle-monitor before signaling.
	// On Unix, check /proc/<pid>/exe or cmdline for the expected binary/state dir.
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if !h.verifyIdleMonitorPID(pid) {
			return // PID does not match expected idle-monitor process
		}
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	time.Sleep(500 * time.Millisecond)
}

// verifyIdleMonitorPID checks if the given PID belongs to the idle-monitor
// process for this harness. Returns true if the process is verified.
func (h *Harness) verifyIdleMonitorPID(pid int) bool {
	// On Linux, read /proc/<pid>/cmdline to verify the process identity.
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	data, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return false // process doesn't exist or can't read
	}
	cmdline := string(data)
	// idle-monitor is invoked with the state directory as an argument.
	// Check if the cmdline contains "idle-monitor" and our StateDir.
	return strings.Contains(cmdline, "idle-monitor") && strings.Contains(cmdline, h.StateDir)
}

// buildSubprocessEnv returns the environment for subprocess invocations.
// Starts from os.Environ() so the subprocess inherits PATH etc., then overrides
// test-specific vars.
func (h *Harness) buildSubprocessEnv() []string {
	env := os.Environ()
	env = setEnv(env, "AIVM_STATE_DIR", h.StateDir)
	env = setEnv(env, "NO_COLOR", "1")
	if h.tc.Interactive {
		env = setEnv(env, "AIVM_FORCE_INTERACTIVE", "1")
	}
	return env
}

// setEnv replaces the first matching KEY=... entry in env, or appends a new one.
func setEnv(env []string, key, val string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random hex: %v", err))
	}
	return hex.EncodeToString(b)
}

// chmodAllWritable recursively makes every file and directory under root
// world-writable (0777) so os.RemoveAll succeeds even when files are owned
// by a different user (e.g. Docker container user vs. host test runner).
// Errors are silently ignored since this is best-effort cleanup preparation.
func chmodAllWritable(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore walk errors
		}
		_ = os.Chmod(path, 0777)
		return nil
	})
}
