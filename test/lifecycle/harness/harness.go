package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/monitor"
	"github.com/sisimomo/aivm/internal/providers/generic"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/t3code"
	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/testvm"
)

type Harness struct {
	t       *testing.T
	svc     *lifecycle.LifecycleService
	fake    *testvm.FakeVM
	cfg     harnessConfig
	cfgPath string
}

type Option func(*harnessConfig)

func WithScriptedAnswers(answers ...string) Option {
	return func(c *harnessConfig) { c.scriptedAnswers = answers }
}

func WithBaseImageEnabled(enabled bool) Option {
	return func(c *harnessConfig) { c.baseImageEnable = enabled }
}

func WithBootstrapRefreshDays(days int) Option {
	return func(c *harnessConfig) {
		if days < 0 {
			c.bootstrapRefreshPromptAfter = "-1"
		} else {
			c.bootstrapRefreshPromptAfter = fmt.Sprintf("%dd", days)
		}
	}
}

func WithRecreatePromptDays(days int) Option {
	return func(c *harnessConfig) {
		if days < 0 {
			c.recreatePromptAfter = "-1"
		} else {
			c.recreatePromptAfter = fmt.Sprintf("%dd", days)
		}
	}
}

func WithBackend(backend string) Option {
	return func(c *harnessConfig) { c.backend = backend }
}

func WithVMType(vmType string) Option {
	return func(c *harnessConfig) { c.vmType = vmType }
}

type noopCompose struct{}

func (noopCompose) Up(context.Context) error { return nil }

func (noopCompose) Down(context.Context) error { return nil }

func (noopCompose) HealthMap(context.Context) map[string]bool { return nil }

func New(t *testing.T, opts ...Option) *Harness {
	t.Helper()
	cfg := defaultHarnessConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	stateDir := t.TempDir()
	cfgPath := filepath.Join(stateDir, "aivm.yaml")
	yaml := buildHarnessYAML(cfg)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	agentDefs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("agent.LoadDefs: %v", err)
	}
	agentReg := agent.NewRegistry()
	for name, def := range agentDefs {
		agentReg.Register(generic.NewFromDef(name, def))
	}

	engine := &config.CompositionEngine{Defaults: config.Defaults{StateDir: stateDir}}
	comp, err := engine.Compose(cfgPath, agentReg)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	for name, def := range comp.CustomAgentDefs {
		agentReg.Register(generic.NewFromDef(name, def))
	}
	comp.Config.StateDir = stateDir

	activeProv := comp.ActiveProvider
	if activeProv == nil {
		activeProv, _ = agentReg.Get(comp.DefaultAgent)
	}

	enabledNames := make([]string, 0, len(comp.EnabledAgentDefs))
	for name := range comp.EnabledAgentDefs {
		enabledNames = append(enabledNames, name)
	}
	sort.Strings(enabledNames)
	enabledProviders := make([]agent.Provider, 0, len(enabledNames))
	for _, name := range enabledNames {
		if p, ok := agentReg.Get(name); ok {
			enabledProviders = append(enabledProviders, p)
		}
	}

	fake := testvm.New()
	sessions := session.NewStore(stateDir)
	composeMgr := noopCompose{}
	mon := monitor.NewIdleMonitor(
		sessions, fake, composeMgr,
		config.DisabledDuration, config.DisabledDuration, stateDir,
	)
	mon.DisableDaemonLaunch = true

	var confirmer lifecycle.Confirmer = &lifecycle.SilentConfirmer{}
	if len(cfg.scriptedAnswers) > 0 {
		confirmer = lifecycle.NewScriptedConfirmer(cfg.scriptedAnswers...)
	}

	svc := &lifecycle.LifecycleService{
		Config:           comp.Config,
		VM:               fake,
		Compose:          composeMgr,
		T3Code:           &t3code.NoopManager{},
		Sessions:         sessions,
		Monitor:          mon,
		Registry:         comp.Plugins,
		Agents:           comp.Agents,
		Provider:         activeProv,
		EnabledProviders: enabledProviders,
		AgentDefs:        comp.EnabledAgentDefs,
		PluginDefs:       comp.PluginDefs,
		Integrations:     comp.Integrations,
		Confirmer:        confirmer,
	}

	return &Harness{t: t, svc: svc, fake: fake, cfg: cfg, cfgPath: cfgPath}
}

func (h *Harness) SVC() *lifecycle.LifecycleService { return h.svc }

func (h *Harness) VM() *testvm.FakeVM { return h.fake }

func (h *Harness) StateDir() string { return h.svc.Config.StateDir }

func effectiveBackend(vmCfg config.VMConfig) string {
	if vmCfg.Backend == "" {
		return "lima"
	}
	return vmCfg.Backend
}

func effectiveVMType(vmCfg config.VMConfig) string {
	if vmCfg.Type != "" {
		return vmCfg.Type
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "vz"
	}
	return "qemu"
}

func (h *Harness) writeBootstrapState(state *lifecycle.BootstrapState) {
	h.t.Helper()
	stateDir := h.StateDir()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		h.t.Fatalf("marshal bootstrap state: %v", err)
	}
	path := filepath.Join(stateDir, "bootstrap-state.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		h.t.Fatalf("write bootstrap state: %v", err)
	}
	vm.RecordBootstrapAt(stateDir)
	vm.RecordVMCreation(stateDir)
}

func (h *Harness) SeedBootstrapped() {
	h.t.Helper()
	h.writeBootstrapState(&lifecycle.BootstrapState{
		Version:    lifecycle.BootstrapVersion,
		Provider:   h.svc.Provider.Name(),
		Backend:    effectiveBackend(h.svc.Config.VM),
		VMType:     effectiveVMType(h.svc.Config.VM),
		ConfigHash: h.svc.CurrentConfigHashForTest(),
		EnvHash:    h.svc.CurrentEnvHashForTest(),
	})
}

func (h *Harness) SeedBootstrapStateWithBackend(backend, vmType string) {
	h.t.Helper()
	h.writeBootstrapState(&lifecycle.BootstrapState{
		Version:    lifecycle.BootstrapVersion,
		Provider:   h.svc.Provider.Name(),
		Backend:    backend,
		VMType:     vmType,
		ConfigHash: h.svc.CurrentConfigHashForTest(),
		EnvHash:    h.svc.CurrentEnvHashForTest(),
	})
}

func (h *Harness) SetBootstrapDaysAgo(days int) {
	h.t.Helper()
	ts := time.Now().AddDate(0, 0, -days).Unix()
	path := filepath.Join(h.StateDir(), vm.BootstrapAtFile)
	payload := []byte(strconv.FormatInt(ts, 10))
	if err := os.WriteFile(path, payload, 0644); err != nil {
		h.t.Fatalf("write bootstrap-at: %v", err)
	}
}

func (h *Harness) SetVMCreatedDaysAgo(days int) {
	h.t.Helper()
	ts := time.Now().AddDate(0, 0, -days).Unix()
	path := filepath.Join(h.StateDir(), vm.VMCreatedAtFile)
	payload := []byte(strconv.FormatInt(ts, 10))
	if err := os.WriteFile(path, payload, 0644); err != nil {
		h.t.Fatalf("write vm-created-at: %v", err)
	}
}

func (h *Harness) SetVMStatus(s vm.Status) {
	h.VM().SetStatus(s)
}

func (h *Harness) SetBaseImage(exists bool) {
	h.VM().SetBaseImageExists(exists)
}

func (h *Harness) StateFileExists(name string) bool {
	h.t.Helper()
	_, err := os.Stat(filepath.Join(h.StateDir(), name))
	return err == nil
}

func (h *Harness) BootstrapState() *lifecycle.BootstrapState {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.StateDir(), "bootstrap-state.json"))
	if err != nil {
		h.t.Fatalf("read bootstrap state: %v", err)
	}
	var state lifecycle.BootstrapState
	if err := json.Unmarshal(data, &state); err != nil {
		h.t.Fatalf("parse bootstrap state: %v", err)
	}
	return &state
}

func (h *Harness) HasBaseImage() bool {
	return h.VM().BaseImageExists()
}

func (h *Harness) BootstrapAtUnix() int64 {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.StateDir(), vm.BootstrapAtFile))
	if err != nil {
		h.t.Fatalf("read bootstrap-at: %v", err)
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		h.t.Fatalf("parse bootstrap-at: %v", err)
	}
	return ts
}
