//go:build bootstrap

package bootstraptest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
)

const maxConcurrentBootstrapHarnesses = 5

var bootstrapHarnessSem = make(chan struct{}, maxConcurrentBootstrapHarnesses)

// BootstrapHarness is a test helper that runs real plugin install scripts,
// agent setups, and integrations inside a fresh Docker container. It uses the
// real plugin.Executor and integration.Executor (no stubs) so template
// rendering, dependency ordering, and the full bootstrap logic are exercised.
type BootstrapHarness struct {
	t                *testing.T
	vm               *vm.DockerVM
	reg              *plugin.Registry
	defs             map[string]plugin.PluginDef
	integDefs        []integration.IntegrationDef
	installedPlugins map[string]bool
	stateDir         string
}

type bootstrapHarnessOptions struct {
	privileged bool
}

// newBootstrapHarness builds a fresh Docker container and loads all real
// plugin, agent, and integration definitions. The container and temp state dir
// are removed automatically when the test ends.
func newBootstrapHarness(t *testing.T) *BootstrapHarness {
	return newBootstrapHarnessWithOptions(t, bootstrapHarnessOptions{})
}

func newPrivilegedBootstrapHarness(t *testing.T) *BootstrapHarness {
	return newBootstrapHarnessWithOptions(t, bootstrapHarnessOptions{privileged: true})
}

func newBootstrapHarnessWithOptions(t *testing.T, opts bootstrapHarnessOptions) *BootstrapHarness {
	t.Helper()
	acquireBootstrapHarness(t)

	if err := framework.BuildTestImage(); err != nil {
		t.Fatalf("harness: ensure test image: %v", err)
	}

	suffix := mustRandomHex(6)
	profile := "aivm-bstest-" + suffix

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("harness: get home dir: %v", err)
	}
	testRunDir := filepath.Join(home, ".aivm", "test-runs", profile)
	stateDir := filepath.Join(testRunDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("harness: create state dir: %v", err)
	}

	dockerVM := vm.NewDocker(profile, stateDir, framework.TestImageName)

	ctx := context.Background()
	if err := dockerVM.Start(ctx, vm.StartOptions{Privileged: opts.privileged}); err != nil {
		t.Fatalf("harness: start container: %v", err)
	}
	if err := dockerVM.WaitReady(ctx, 30*time.Second); err != nil {
		t.Fatalf("harness: wait container ready: %v", err)
	}

	// Load real plugin definitions.
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load plugin defaults: %v", err)
	}

	// Merge agent definitions as plugins (claude, copilot).
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("harness: load agent defaults: %v", err)
	}
	for name, def := range agentDefs {
		defs[name] = def.ToPluginDef()
	}

	reg := plugin.NewRegistry()
	for name, def := range defs {
		reg.Set(plugin.NewYAMLPlugin(name, def))
	}

	// Load integration definitions.
	integDefs, err := integration.LoadDefaults()
	if err != nil {
		t.Fatalf("harness: load integration defaults: %v", err)
	}

	h := &BootstrapHarness{
		t:                t,
		vm:               dockerVM,
		reg:              reg,
		defs:             defs,
		integDefs:        integDefs,
		installedPlugins: make(map[string]bool),
		stateDir:         stateDir,
	}

	t.Cleanup(func() {
		_ = dockerVM.Destroy(context.Background())
		os.RemoveAll(testRunDir) //nolint:errcheck
	})

	return h
}

func acquireBootstrapHarness(t *testing.T) {
	t.Helper()
	bootstrapHarnessSem <- struct{}{}
	t.Cleanup(func() {
		<-bootstrapHarnessSem
	})
}

// Install runs the named plugin (and all its transitive dependencies) inside
// the container using the real bootstrap engine. cfg overrides per-plugin
// config for the plugin under test; dependencies use their own defaults.
//
// All transitively installed plugins are recorded so that a subsequent
// RunIntegrations call can accurately match From conditions.
func (h *BootstrapHarness) Install(pluginName string, cfg map[string]any) {
	h.t.Helper()
	pluginConfigs := map[string]map[string]any{}
	if len(cfg) > 0 {
		pluginConfigs[pluginName] = cfg
	}

	exec := &plugin.Executor{
		Registry:     h.reg,
		Enabled:      []string{pluginName},
		PluginConfig: pluginConfigs,
		StateDir:     h.stateDir,
		VMInst:       h.vm,
	}

	// Resolve the full execution order before running so we can record every
	// installed plugin (including transitive deps) for later RunIntegrations.
	ordered, err := exec.Ordered()
	if err != nil {
		h.t.Fatalf("Install %q: resolve order: %v", pluginName, err)
	}

	if err := exec.Run(context.Background()); err != nil {
		h.t.Fatalf("Install %q: %v", pluginName, err)
	}

	for _, p := range ordered {
		h.installedPlugins[p.Name()] = true
	}
}

// RunIntegrations executes all integrations from the embedded defaults whose
// From/To conditions match the plugins installed so far and the given active
// agent. templateVars are substituted into configure scripts
// (e.g. map[string]any{"mcp_port": "8472"}).
//
// It returns the keys of integrations that were executed.
func (h *BootstrapHarness) RunIntegrations(agentName string, templateVars map[string]any) []string {
	h.t.Helper()

	exec := &integration.Executor{
		Integrations:     h.integDefs,
		InstalledPlugins: h.installedPlugins,
		ActiveAgents:     []string{agentName},
		VM:               h.vm,
		Log:              os.Stderr,
		TemplateVars:     templateVars,
	}

	ran, err := exec.Run(context.Background())
	if err != nil {
		h.t.Fatalf("RunIntegrations %q: %v", agentName, err)
	}
	return ran
}

// AssertCommand runs cmd inside the container as a login shell and asserts that
// the combined output contains wantSubstr. Fails the test on error or mismatch.
func (h *BootstrapHarness) AssertCommand(cmd, wantSubstr string) {
	h.t.Helper()
	output, err := h.vm.RunOutput(context.Background(), cmd, nil)
	if err != nil {
		h.t.Fatalf("AssertCommand %q: command failed: %v\noutput:\n%s", cmd, err, output)
	}
	if wantSubstr != "" && !strings.Contains(output, wantSubstr) {
		h.t.Fatalf("AssertCommand %q: output does not contain %q\nfull output:\n%s", cmd, wantSubstr, output)
	}
}

// AssertLaunchStartsTUI runs the agent's launch_command inside the container
// for a short duration and asserts that the process does not exit immediately
// with an error. A TUI application should stay alive until killed; if the
// launch_command contains an unrecognised flag the binary will exit non-zero
// before the timeout, which is the observable symptom of the bug where the
// opencode TUI never opens.
//
// The method kills the process after probeTimeout by cancelling the context.
// It then checks that the resulting error is a context/signal error and not an
// early exit error (exit status != 0 before the deadline).
func (h *BootstrapHarness) AssertLaunchStartsTUI(agentName string, probeTimeout time.Duration) {
	h.t.Helper()

	defs, err := agent.LoadDefs()
	if err != nil {
		h.t.Fatalf("AssertLaunchStartsTUI: load agent defs: %v", err)
	}
	def, ok := defs[agentName]
	if !ok {
		h.t.Fatalf("AssertLaunchStartsTUI: agent %q not found", agentName)
	}
	if def.CLICommand == "" {
		h.t.Fatalf("AssertLaunchStartsTUI: agent %q has empty cli_command", agentName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// RunOutput returns an error when the process exits. We distinguish:
	//   - context.DeadlineExceeded / signal kill → TUI was alive, test passes
	//   - immediate non-zero exit → launch_command failed, test fails
	start := time.Now()
	workDir := "/tmp"
	script := vm.BuildLaunchScript(workDir, def.CLICommand, def.LaunchArgs)
	_, runErr := h.vm.RunOutput(ctx, script, nil)
	elapsed := time.Since(start)

	if runErr == nil {
		h.t.Errorf("AssertLaunchStartsTUI %q: launch script exited cleanly after %v — expected it to stay alive for ~%v",
			agentName, elapsed, probeTimeout)
		return
	}

	// If the process was killed because the context expired, that is the
	// expected "TUI stayed alive" outcome.
	if ctx.Err() == context.DeadlineExceeded {
		return
	}

	// The process exited well before the timeout with an error → the
	// launch_command is broken (e.g. unrecognised flag).
	h.t.Errorf("AssertLaunchStartsTUI %q: launch script exited after %v with error %v — "+
		"the TUI did not start (expected the process to stay alive for ~%v)",
		agentName, elapsed, runErr, probeTimeout)
}

func mustRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random hex: %v", err))
	}
	return hex.EncodeToString(b)
}
