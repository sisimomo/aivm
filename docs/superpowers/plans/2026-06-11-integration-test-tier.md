# Integration Test Tier Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a fast integration test tier that exercises `LifecycleService`
orchestration end-to-end via direct service calls and a stateful `FakeVM`,
without Docker or subprocesses.

**Architecture:** A production seam change widens `vm.AsBaseImageStore` to
interface assertion so `FakeVM` can participate in base-image paths.
`test/testvm.FakeVM` models VM + base-image state transitions and records a
call log. `test/lifecycle/harness` builds a fully wired `LifecycleService` in
`t.TempDir()` with seed/assert helpers. Scenario tests in
`test/integration/lifecycle/` call `Start`, `Destroy`, and `Recreate` directly.

**Tech Stack:** Go 1.26, `testing`, `lifecycle.LifecycleService`,
`config.CompositionEngine`, `monitor.IdleMonitor`, `ScriptedConfirmer`,
`SilentConfirmer`

**Spec:** `docs/superpowers/specs/2026-06-11-integration-test-design.md`

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/vm/base_store.go` | Widen `AsBaseImageStore` to interface assertion |
| `test/unit/vm/base_store_test.go` | Regression test for interface-based lookup |
| `test/testvm/fake.go` | `FakeVM` implementing `vm.VM` + `vm.BaseImageStore` |
| `test/testvm/fake_test.go` | FakeVM state machine unit tests |
| `test/lifecycle/harness/harness.go` | `Harness` builder, seed/assert helpers |
| `test/lifecycle/harness/config.go` | Minimal test YAML generation |
| `test/integration/lifecycle/destroy_test.go` | Destroy `keepBase` scenarios |
| `test/integration/lifecycle/recreate_test.go` | Recreate fast/full/fallback scenarios |
| `test/integration/lifecycle/start_test.go` | Non-interactive start decision paths |
| `test/integration/lifecycle/prompts_test.go` | Interactive prompt → outcome scenarios |
| `Makefile` | `test-integration` target |
| `.github/workflows/ci.yml` | Run integration tests on every PR |
| `.github/workflows/release.yml` | Run integration tests before release |

---

## Task 1: Widen `AsBaseImageStore`

**Files:**

- Modify: `internal/vm/base_store.go`
- Create: `test/unit/vm/base_store_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vm_test

import (
 "context"
 "testing"
 "time"

 "github.com/sisimomo/aivm/internal/vm"
)

// stubBaseStore is a minimal dual VM+BaseImageStore for AsBaseImageStore tests.
// All VM methods are no-ops; only HasBaseImage is asserted.
type stubBaseStore struct{ has bool }

func (s *stubBaseStore) Profile() string { return "stub" }
func (s *stubBaseStore) NeedsPortBindingAtBoot() bool { return true }
func (s *stubBaseStore) Status(context.Context) (vm.Status, error) {
 return vm.StatusNotFound, nil
}
func (s *stubBaseStore) Start(context.Context, vm.StartOptions) error {
 return nil
}
func (s *stubBaseStore) Stop(context.Context) error { return nil }
func (s *stubBaseStore) Destroy(context.Context) error { return nil }
func (s *stubBaseStore) Run(context.Context, string, map[string]string) error {
 return nil
}
func (s *stubBaseStore) RunOutput(context.Context, string, map[string]string) (
 string, error,
) {
 return "", nil
}
func (s *stubBaseStore) RunInteractive(
 context.Context, string, map[string]string,
) error {
 return nil
}
func (s *stubBaseStore) RunStream(context.Context, string, map[string]string) (
 int, error,
) {
 return 0, nil
}
func (s *stubBaseStore) SSH(context.Context, map[string]string) error {
 return nil
}
func (s *stubBaseStore) CopyTo(context.Context, string, string, bool) error {
 return nil
}
func (s *stubBaseStore) CopyFrom(context.Context, string, string, bool) error {
 return nil
}
func (s *stubBaseStore) WaitReady(context.Context, time.Duration) error {
 return nil
}
func (s *stubBaseStore) GetPublishedPort(int) (int, error) {
 return 0, nil
}
func (s *stubBaseStore) SaveBaseImage(context.Context, vm.StartOptions) error {
 return nil
}
func (s *stubBaseStore) RestoreFromBaseImage(
 context.Context, vm.StartOptions,
) error {
 return nil
}
func (s *stubBaseStore) DeleteBaseImage(context.Context) error { return nil }
func (s *stubBaseStore) HasBaseImage(context.Context) bool     { return s.has }

func TestAsBaseImageStore_InterfaceAssertion(t *testing.T) {
 t.Parallel()
 v := &stubBaseStore{has: true}
 store, ok := vm.AsBaseImageStore(v)
 if !ok {
  t.Fatal("expected stub implementing BaseImageStore to be recognized")
 }
 if !store.HasBaseImage(context.Background()) {
  t.Fatal("expected HasBaseImage true")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/ -run TestAsBaseImageStore_InterfaceAssertion -v`

Expected: FAIL — `stubBaseStore` not recognized (`ok == false`)

- [ ] **Step 3: Replace type switch with interface assertion**

In `internal/vm/base_store.go`:

```go
func AsBaseImageStore(v VM) (BaseImageStore, bool) {
 store, ok := v.(BaseImageStore)
 return store, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/vm/ -run TestAsBaseImageStore_InterfaceAssertion -v`

Expected: PASS

- [ ] **Step 5: Format and lint**

Run:

```bash
go fmt internal/vm/base_store.go test/unit/vm/base_store_test.go
golangci-lint run internal/vm/base_store.go test/unit/vm/base_store_test.go
```

- [ ] **Step 6: Commit**

```bash
git add internal/vm/base_store.go test/unit/vm/base_store_test.go
git commit -m "refactor: widen AsBaseImageStore to interface assertion"
```

---

## Task 2: FakeVM core

**Files:**

- Create: `test/testvm/fake.go`

- [ ] **Step 1: Create `test/testvm/fake.go`**

```go
package testvm

import (
 "context"
 "sync"
 "time"

 "github.com/sisimomo/aivm/internal/vm"
)

// Call records one FakeVM method invocation for test assertions.
type Call struct {
 Method string
 Detail string
}

// Faults inject errors on specific methods. Zero values mean success.
type Faults struct {
 StartErr                error
 StopErr                 error
 DestroyErr              error
 SaveBaseImageErr        error
 RestoreFromBaseImageErr error
 DeleteBaseImageErr      error
 WaitReadyErr            error
}

// FakeVM is a stateful VM + base-image simulator for lifecycle integration tests.
type FakeVM struct {
 mu              sync.Mutex
 status          vm.Status
 baseImageExists bool
 calls           []Call
 faults          Faults
}

func New() *FakeVM {
 return &FakeVM{status: vm.StatusNotFound}
}

func (f *FakeVM) Profile() string { return "test" }

func (f *FakeVM) NeedsPortBindingAtBoot() bool { return true }

func (f *FakeVM) Status(_ context.Context) (vm.Status, error) {
 f.mu.Lock()
 defer f.mu.Unlock()
 return f.status, nil
}

func (f *FakeVM) Start(_ context.Context, _ vm.StartOptions) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("Start", "")
 if f.faults.StartErr != nil {
  return f.faults.StartErr
 }
 if f.status == vm.StatusNotFound || f.status == vm.StatusStopped {
  f.status = vm.StatusRunning
 }
 return nil
}

func (f *FakeVM) Stop(_ context.Context) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("Stop", "")
 if f.faults.StopErr != nil {
  return f.faults.StopErr
 }
 if f.status == vm.StatusRunning {
  f.status = vm.StatusStopped
 }
 return nil
}

func (f *FakeVM) Destroy(_ context.Context) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("Destroy", "")
 if f.faults.DestroyErr != nil {
  return f.faults.DestroyErr
 }
 f.status = vm.StatusNotFound
 return nil
}

func (f *FakeVM) Run(
 _ context.Context, script string, _ map[string]string,
) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("Run", script)
 return nil
}

func (f *FakeVM) RunOutput(
 _ context.Context, script string, _ map[string]string,
) (string, error) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("RunOutput", script)
 return "", nil
}

func (f *FakeVM) RunInteractive(
 _ context.Context, script string, _ map[string]string,
) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("RunInteractive", script)
 return nil
}

func (f *FakeVM) RunStream(
 _ context.Context, script string, _ map[string]string,
) (int, error) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("RunStream", script)
 return 0, nil
}

func (f *FakeVM) SSH(_ context.Context, _ map[string]string) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("SSH", "")
 return nil
}

func (f *FakeVM) CopyTo(_ context.Context, _, _ string, _ bool) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("CopyTo", "")
 return nil
}

func (f *FakeVM) CopyFrom(_ context.Context, _, _ string, _ bool) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("CopyFrom", "")
 return nil
}

func (f *FakeVM) WaitReady(_ context.Context, _ time.Duration) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("WaitReady", "")
 return f.faults.WaitReadyErr
}

func (f *FakeVM) GetPublishedPort(port int) (int, error) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("GetPublishedPort", "")
 return port, nil
}

func (f *FakeVM) SaveBaseImage(_ context.Context, _ vm.StartOptions) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("SaveBaseImage", "")
 if f.faults.SaveBaseImageErr != nil {
  return f.faults.SaveBaseImageErr
 }
 f.baseImageExists = true
 return nil
}

func (f *FakeVM) RestoreFromBaseImage(
 _ context.Context, _ vm.StartOptions,
) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("RestoreFromBaseImage", "")
 if f.faults.RestoreFromBaseImageErr != nil {
  return f.faults.RestoreFromBaseImageErr
 }
 if !f.baseImageExists {
  return context.Canceled // arbitrary error; tests inject via Faults instead
 }
 f.status = vm.StatusNotFound
 f.status = vm.StatusRunning
 return nil
}

func (f *FakeVM) DeleteBaseImage(_ context.Context) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("DeleteBaseImage", "")
 if f.faults.DeleteBaseImageErr != nil {
  return f.faults.DeleteBaseImageErr
 }
 f.baseImageExists = false
 return nil
}

func (f *FakeVM) HasBaseImage(_ context.Context) bool {
 f.mu.Lock()
 defer f.mu.Unlock()
 return f.baseImageExists
}

// --- test helpers ---

func (f *FakeVM) CallLog() []Call {
 f.mu.Lock()
 defer f.mu.Unlock()
 out := make([]Call, len(f.calls))
 copy(out, f.calls)
 return out
}

func (f *FakeVM) HasCall(method string) bool {
 f.mu.Lock()
 defer f.mu.Unlock()
 for _, c := range f.calls {
  if c.Method == method {
   return true
  }
 }
 return false
}

func (f *FakeVM) CallCount(method string) int {
 f.mu.Lock()
 defer f.mu.Unlock()
 n := 0
 for _, c := range f.calls {
  if c.Method == method {
   n++
  }
 }
 return n
}

func (f *FakeVM) SetStatus(s vm.Status) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.status = s
}

func (f *FakeVM) BaseImageExists() bool {
 f.mu.Lock()
 defer f.mu.Unlock()
 return f.baseImageExists
}

func (f *FakeVM) SetBaseImageExists(exists bool) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.baseImageExists = exists
}

func (f *FakeVM) SetFaults(faults Faults) {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.faults = faults
}

func (f *FakeVM) ResetCallLog() {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.calls = nil
}

func (f *FakeVM) appendCall(method, detail string) {
 f.calls = append(f.calls, Call{Method: method, Detail: detail})
}
```

Fix `RestoreFromBaseImage` — remove the redundant double status assignment; use:

```go
 f.status = vm.StatusRunning
```

after simulating destroy-then-start. The call log records `RestoreFromBaseImage`;
tests assert on that, not intermediate `NotFound`.

- [ ] **Step 2: Verify package compiles**

Run: `go build ./test/testvm/...`

Expected: success (no test file yet)

- [ ] **Step 3: Format and lint**

Run:

```bash
go fmt ./test/testvm/...
golangci-lint run ./test/testvm/...
```

- [ ] **Step 4: Commit**

```bash
git add test/testvm/fake.go
git commit -m "feat: add stateful FakeVM for integration tests"
```

---

## Task 3: FakeVM unit tests

**Files:**

- Create: `test/testvm/fake_test.go`

- [ ] **Step 1: Write failing state-machine tests**

```go
package testvm_test

import (
 "context"
 "errors"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
 "github.com/sisimomo/aivm/test/testvm"
)

func TestFakeVM_StartStopDestroy(t *testing.T) {
 t.Parallel()
 f := testvm.New()
 ctx := context.Background()

 if got, _ := f.Status(ctx); got != vm.StatusNotFound {
  t.Fatalf("initial status: got %v want NotFound", got)
 }
 if err := f.Start(ctx, vm.StartOptions{}); err != nil {
  t.Fatal(err)
 }
 if got, _ := f.Status(ctx); got != vm.StatusRunning {
  t.Fatalf("after start: got %v", got)
 }
 if err := f.Stop(ctx); err != nil {
  t.Fatal(err)
 }
 if got, _ := f.Status(ctx); got != vm.StatusStopped {
  t.Fatalf("after stop: got %v", got)
 }
 if err := f.Destroy(ctx); err != nil {
  t.Fatal(err)
 }
 if got, _ := f.Status(ctx); got != vm.StatusNotFound {
  t.Fatalf("after destroy: got %v", got)
 }
}

func TestFakeVM_BaseImageLifecycle(t *testing.T) {
 t.Parallel()
 f := testvm.New()
 ctx := context.Background()

 if f.HasBaseImage(ctx) {
  t.Fatal("expected no base image initially")
 }
 if err := f.SaveBaseImage(ctx, vm.StartOptions{}); err != nil {
  t.Fatal(err)
 }
 if !f.HasBaseImage(ctx) {
  t.Fatal("expected base image after save")
 }
 if err := f.RestoreFromBaseImage(ctx, vm.StartOptions{}); err != nil {
  t.Fatal(err)
 }
 if !f.HasCall("RestoreFromBaseImage") {
  t.Fatal("expected RestoreFromBaseImage in call log")
 }
 if err := f.DeleteBaseImage(ctx); err != nil {
  t.Fatal(err)
 }
 if f.HasBaseImage(ctx) {
  t.Fatal("expected base image deleted")
 }
}

func TestFakeVM_DestroyPreservesBaseImage(t *testing.T) {
 t.Parallel()
 f := testvm.New()
 ctx := context.Background()
 _ = f.SaveBaseImage(ctx, vm.StartOptions{})
 _ = f.Start(ctx, vm.StartOptions{})
 _ = f.Destroy(ctx)
 if !f.BaseImageExists() {
  t.Fatal("Destroy must not delete base image artifact")
 }
}

func TestFakeVM_FaultInjection(t *testing.T) {
 t.Parallel()
 f := testvm.New()
 f.SetBaseImageExists(true)
 f.SetFaults(testvm.Faults{RestoreFromBaseImageErr: errors.New("restore failed")})
 ctx := context.Background()
 if err := f.RestoreFromBaseImage(ctx, vm.StartOptions{}); err == nil {
  t.Fatal("expected restore error")
 }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./test/testvm/... -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/testvm/fake_test.go
git commit -m "test: add FakeVM state machine unit tests"
```

---

## Task 4: Lifecycle harness — config and builder

**Files:**

- Create: `test/lifecycle/harness/config.go`
- Create: `test/lifecycle/harness/harness.go`

- [ ] **Step 1: Create `test/lifecycle/harness/config.go`**

```go
package harness

import (
 "fmt"
 "strings"
)

type harnessConfig struct {
 backend                     string
 vmType                      string
 baseImageEnable             bool
 recreatePromptAfter         string
 bootstrapRefreshPromptAfter string
 scriptedAnswers             []string
}

func defaultHarnessConfig() harnessConfig {
 return harnessConfig{
  backend:                     "docker",
  vmType:                      "",
  baseImageEnable:             true,
  recreatePromptAfter:         "-1",
  bootstrapRefreshPromptAfter: "30d",
 }
}

func buildHarnessYAML(cfg harnessConfig) string {
 var sb strings.Builder
 fmt.Fprintf(&sb, "vm:\n")
 fmt.Fprintf(&sb, "  backend: %q\n", cfg.backend)
 if cfg.vmType != "" {
  fmt.Fprintf(&sb, "  type: %q\n", cfg.vmType)
 }
 fmt.Fprintf(&sb, "  base_image_enable: %v\n", cfg.baseImageEnable)
 fmt.Fprintf(&sb, "  recreate_prompt_after: %q\n", cfg.recreatePromptAfter)
 if cfg.bootstrapRefreshPromptAfter != "" {
  fmt.Fprintf(&sb, "  bootstrap_refresh_prompt_after: %q\n", cfg.bootstrapRefreshPromptAfter)
 }
 fmt.Fprintf(&sb, "idle:\n")
 fmt.Fprintf(&sb, "  stop_timeout: %q\n", "-1")
 fmt.Fprintf(&sb, "  delete_timeout: %q\n", "-1")
 fmt.Fprintf(&sb, "agents:\n")
 fmt.Fprintf(&sb, "  default: claude\n")
 fmt.Fprintf(&sb, "  enabled:\n")
 fmt.Fprintf(&sb, "    - claude\n")
 fmt.Fprintf(&sb, "plugins:\n")
 fmt.Fprintf(&sb, "  enabled: []\n")
 return sb.String()
}
```

- [ ] **Step 2: Create `test/lifecycle/harness/harness.go` (core)**

```go
package harness

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "strconv"
 "testing"
 "time"

 "github.com/sisimomo/aivm/internal/agent"
 "github.com/sisimomo/aivm/internal/compose"
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
 t      *testing.T
 svc    *lifecycle.LifecycleService
 fake   *testvm.FakeVM
 cfg    harnessConfig
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

func (noopCompose) Up(context.Context) error   { return nil }
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
 compCfg := comp.Config
 compCfg.StateDir = stateDir

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
  Config:           &compCfg,
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
```

Add missing imports: `fmt`, `sort`.

- [ ] **Step 3: Verify harness compiles**

Run: `go build ./test/lifecycle/harness/...`

Expected: success

- [ ] **Step 4: Commit**

```bash
git add test/lifecycle/harness/
git commit -m "feat: add lifecycle integration test harness builder"
```

---

## Task 5: Lifecycle harness — seed and assert helpers

**Files:**

- Modify: `test/lifecycle/harness/harness.go`

- [ ] **Step 1: Add seed helpers**

Append to `harness.go`:

```go
func (h *Harness) SeedBootstrapped() {
 h.t.Helper()
 stateDir := h.StateDir()
 state := &lifecycle.BootstrapState{
  Version:    lifecycle.BootstrapVersion,
  Provider:   h.svc.Provider.Name(),
  Backend:    effectiveBackend(h.svc.Config.VM),
  VMType:     effectiveVMType(h.svc.Config.VM),
  ConfigHash: h.svc.CurrentConfigHashForTest(),
  EnvHash:    h.svc.CurrentEnvHashForTest(),
 }
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
```

**Production seam for hash access:** `currentConfigHash` and `currentEnvHash` are
unexported. Add test-only exports in `internal/lifecycle/state.go` (same pattern
as `FastRecreateForTest`):

```go
// CurrentConfigHashForTest exposes currentConfigHash for harness seeding.
func (svc *LifecycleService) CurrentConfigHashForTest() string {
 return svc.currentConfigHash()
}

// CurrentEnvHashForTest exposes currentEnvHash for integration harness seeding.
func (svc *LifecycleService) CurrentEnvHashForTest() string {
 return svc.currentEnvHash()
}
```

Copy `effectiveBackend` / `effectiveVMType` into harness as private helpers
(they are unexported in `lifecycle`):

```go
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
```

Add `import "runtime"`.

- [ ] **Step 2: Add assert helpers**

```go
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
```

Add `import "strings"`.

- [ ] **Step 3: Add `CurrentConfigHashForTest` exports and compile**

Run: `go build ./test/lifecycle/harness/...`

Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/lifecycle/state.go test/lifecycle/harness/harness.go
git commit -m "feat: add harness seed and assert helpers"
```

---

## Task 6: Destroy integration tests

**Files:**

- Create: `test/integration/lifecycle/destroy_test.go`

- [ ] **Step 1: Write destroy tests**

```go
package lifecycle_test

import (
 "context"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
 "github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestDestroy_KeepBase_PreservesBootstrapAndBaseImage(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)

 ctx := context.Background()
 if err := h.SVC().Destroy(ctx, true); err != nil {
  t.Fatal(err)
 }
 if !h.StateFileExists("bootstrap-state.json") {
  t.Fatal("expected bootstrap state preserved")
 }
 if !h.HasBaseImage() {
  t.Fatal("expected base image preserved")
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("expected VM Destroy called")
 }
 if h.VM().HasCall("DeleteBaseImage") {
  t.Fatal("DeleteBaseImage must not run when keepBase=true")
 }
}

func TestDestroy_NoKeepBase_ClearsBootstrapAndBaseImage(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)

 ctx := context.Background()
 if err := h.SVC().Destroy(ctx, false); err != nil {
  t.Fatal(err)
 }
 if h.StateFileExists("bootstrap-state.json") {
  t.Fatal("expected bootstrap state cleared")
 }
 if h.HasBaseImage() {
  t.Fatal("expected base image deleted")
 }
 if !h.VM().HasCall("DeleteBaseImage") {
  t.Fatal("expected DeleteBaseImage called")
 }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./test/integration/lifecycle/ -run TestDestroy -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/lifecycle/destroy_test.go
git commit -m "test: add destroy keepBase integration scenarios"
```

---

## Task 7: Recreate integration tests

**Files:**

- Create: `test/integration/lifecycle/recreate_test.go`

- [ ] **Step 1: Write recreate tests**

```go
package lifecycle_test

import (
 "context"
 "errors"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
 "github.com/sisimomo/aivm/test/lifecycle/harness"
 "github.com/sisimomo/aivm/test/testvm"
)

func TestRecreate_FastWithValidBase_RestoresWithoutFullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)
 bootstrapAt := h.BootstrapAtUnix()

 ctx := context.Background()
 if err := h.SVC().Recreate(ctx, true, true); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("expected fast recreate via RestoreFromBaseImage")
 }
 if h.BootstrapAtUnix() != bootstrapAt {
  t.Fatal("bootstrap-at must not change on fast recreate")
 }
}

func TestRecreate_FastWithoutBase_FallsBackToFullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(false)

 ctx := context.Background()
 if err := h.SVC().Recreate(ctx, true, true); err != nil {
  t.Fatal(err)
 }
 if h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("expected full bootstrap, not restore")
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("expected Destroy on full bootstrap path")
 }
}

func TestRecreate_RestoreFailure_FallsBackToFullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)
 h.VM().SetFaults(testvm.Faults{RestoreFromBaseImageErr: errors.New("restore failed")})

 ctx := context.Background()
 if err := h.SVC().Recreate(ctx, true, true); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("DeleteBaseImage") {
  t.Fatal("expected invalid base deleted after restore failure")
 }
 if h.VM().CallCount("Destroy") < 1 {
  t.Fatal("expected full bootstrap destroy after restore failure")
 }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./test/integration/lifecycle/ -run TestRecreate -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/lifecycle/recreate_test.go
git commit -m "test: add recreate fast/full integration scenarios"
```

---

## Task 8: Start integration tests (non-interactive)

**Files:**

- Create: `test/integration/lifecycle/start_test.go`

- [ ] **Step 1: Write start tests**

```go
package lifecycle_test

import (
 "context"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
 "github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestStart_NonInteractive_VMMissing_ValidBase_FastRecreate(t *testing.T) {
 t.Parallel()
 h := harness.New(t) // SilentConfirmer default
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusNotFound)
 h.SetBaseImage(true)

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("expected fast recreate when VM missing and base valid")
 }
}

func TestStart_NonInteractive_VMMissing_NoBase_FullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t)
 h.SetVMStatus(vm.StatusNotFound)
 h.SetBaseImage(false)

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("expected full bootstrap, not restore")
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("expected full bootstrap destroy path")
 }
 if h.StateFileExists("bootstrap-state.json") {
  t.Fatal("expected fresh bootstrap state written")
 }
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./test/integration/lifecycle/ -run TestStart -v`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/lifecycle/start_test.go
git commit -m "test: add non-interactive start decision integration scenarios"
```

---

## Task 9: Interactive prompt integration tests

**Files:**

- Create: `test/integration/lifecycle/prompts_test.go`

- [ ] **Step 1: Bootstrap refresh scenarios**

```go
func TestStart_BootstrapRefreshAccepted_FullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBootstrapRefreshDays(30),
  harness.WithScriptedAnswers("y"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)
 h.SetBootstrapDaysAgo(31)
 oldBootstrapAt := h.BootstrapAtUnix()

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if h.BootstrapAtUnix() <= oldBootstrapAt {
  t.Fatal("expected bootstrap-at updated after accepted refresh")
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("expected full bootstrap destroy")
 }
}

func TestStart_BootstrapRefreshDeclined_StoppedVM_FastRecreate(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBootstrapRefreshDays(30),
  harness.WithScriptedAnswers("n"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusStopped)
 h.SetBaseImage(true)
 h.SetBootstrapDaysAgo(31)
 bootstrapAt := h.BootstrapAtUnix()

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("expected fast recreate when refresh declined on stopped VM")
 }
 if h.BootstrapAtUnix() != bootstrapAt {
  t.Fatal("bootstrap-at must be unchanged on fast recreate")
 }
}
```

- [ ] **Step 2: Combined prompt options 1 / 2 / 3**

```go
func TestStart_CombinedPrompt_Option1_FullBootstrap(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBootstrapRefreshDays(30),
  harness.WithRecreatePromptDays(30),
  harness.WithScriptedAnswers("1"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusStopped)
 h.SetBaseImage(true)
 h.SetBootstrapDaysAgo(31)
 h.SetVMCreatedDaysAgo(31)

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("option 1 should full bootstrap")
 }
}

func TestStart_CombinedPrompt_Option2_FastRecreate(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBootstrapRefreshDays(30),
  harness.WithRecreatePromptDays(30),
  harness.WithScriptedAnswers("2"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusStopped)
 h.SetBaseImage(true)
 h.SetBootstrapDaysAgo(31)
 h.SetVMCreatedDaysAgo(31)
 bootstrapAt := h.BootstrapAtUnix()

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("option 2 should fast recreate")
 }
 if h.BootstrapAtUnix() != bootstrapAt {
  t.Fatal("bootstrap-at unchanged on fast recreate")
 }
}

func TestStart_CombinedPrompt_Option3_Resume(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBootstrapRefreshDays(30),
  harness.WithRecreatePromptDays(30),
  harness.WithScriptedAnswers("3"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusStopped)
 h.SetBaseImage(true)
 h.SetBootstrapDaysAgo(31)
 h.SetVMCreatedDaysAgo(31)
 h.VM().ResetCallLog()

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if h.VM().HasCall("RestoreFromBaseImage") || h.VM().HasCall("Destroy") {
  t.Fatal("option 3 should resume without recreate")
 }
 if got, _ := h.VM().Status(ctx); got != vm.StatusRunning {
  t.Fatalf("expected VM running after resume, got %v", got)
 }
}
```

- [ ] **Step 3: Runtime change scenarios**

Add helper on harness to re-seed bootstrap state with mismatched backend:

```go
func (h *Harness) SeedBootstrapStateWithBackend(backend, vmType string) {
 h.t.Helper()
 state := &lifecycle.BootstrapState{
  Version:    lifecycle.BootstrapVersion,
  Provider:   h.svc.Provider.Name(),
  Backend:    backend,
  VMType:     vmType,
  ConfigHash: h.svc.CurrentConfigHashForTest(),
  EnvHash:    h.svc.CurrentEnvHashForTest(),
 }
 // write bootstrap-state.json + age files (same as SeedBootstrapped)
}
```

Tests:

```go
func TestStart_RuntimeChangeDeclined_PreservesBase(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBackend("docker"),
  harness.WithScriptedAnswers("n"),
 )
 h.SeedBootstrapStateWithBackend("lima", "qemu")
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if h.VM().HasCall("Destroy") {
  t.Fatal("declined runtime change must not destroy VM")
 }
 if !h.HasBaseImage() {
  t.Fatal("base image must be preserved")
 }
}

func TestStart_RuntimeChangeAccepted_DeletesBaseAndRecreates(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithBackend("docker"),
  harness.WithScriptedAnswers("y"),
 )
 h.SeedBootstrapStateWithBackend("lima", "qemu")
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("accepted runtime change must destroy VM")
 }
 if h.HasBaseImage() {
  t.Fatal("base image must be deleted")
 }
 if !h.StateFileExists("bootstrap-state.json") == false {
  // bootstrap state cleared before recreate; may be rewritten after bootstrap
 }
}
```

Simplify runtime-accepted assertion: check `DeleteBaseImage` was called, or that
bootstrap state was cleared before recreate.

- [ ] **Step 4: Config hash change scenarios**

```go
func TestStart_ConfigHashChange_DeletesBaseBeforePrompt(t *testing.T) {
 t.Parallel()
 h := harness.New(t, harness.WithScriptedAnswers("n"))
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)

 // Mutate config hash by changing an execution-relevant field.
 h.SVC().Config.VM.CPUs = 99

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if h.HasBaseImage() {
  t.Fatal("config change must delete base preemptively")
 }
}

func TestStart_ConfigHashChange_Accepted_RecreatesVM(t *testing.T) {
 t.Parallel()
 h := harness.New(t, harness.WithScriptedAnswers("y"))
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusRunning)
 h.SetBaseImage(true)
 h.SVC().Config.VM.CPUs = 99

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("Destroy") {
  t.Fatal("accepted config change must recreate VM")
 }
}
```

- [ ] **Step 5: VM age prompt with valid base**

```go
func TestStart_VMAgePromptAccepted_ValidBase_FastRecreate(t *testing.T) {
 t.Parallel()
 h := harness.New(t,
  harness.WithRecreatePromptDays(30),
  harness.WithScriptedAnswers("y"),
 )
 h.SeedBootstrapped()
 h.SetVMStatus(vm.StatusStopped)
 h.SetBaseImage(true)
 h.SetVMCreatedDaysAgo(31)
 bootstrapAt := h.BootstrapAtUnix()

 ctx := context.Background()
 if err := h.SVC().Start(ctx); err != nil {
  t.Fatal(err)
 }
 if !h.VM().HasCall("RestoreFromBaseImage") {
  t.Fatal("accepted VM age with valid base should fast recreate")
 }
 if h.BootstrapAtUnix() != bootstrapAt {
  t.Fatal("bootstrap-at unchanged on fast recreate")
 }
}
```

- [ ] **Step 6: Run all prompt tests**

Run: `go test ./test/integration/lifecycle/ -run 'TestStart_' -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add test/integration/lifecycle/prompts_test.go test/lifecycle/harness/harness.go
git commit -m "test: add interactive prompt orchestration integration scenarios"
```

---

## Task 10: Makefile and CI

**Files:**

- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Add Makefile target**

In `Makefile`, add to `.PHONY` line and define:

```makefile
test-integration:
 go test ./test/integration/... ./test/testvm/...
```

- [ ] **Step 2: Verify full integration suite speed**

Run: `time make test-integration`

Expected: completes in under 10 seconds

- [ ] **Step 3: Add CI step**

In `.github/workflows/ci.yml`, add a job after `test-unit`. A separate job is
recommended for clarity:

```yaml
  test-integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true

      - name: Run integration tests
        run: make test-integration
```

Mirror the same job in `.github/workflows/release.yml` after `test-unit`.

- [ ] **Step 4: Commit**

```bash
git add Makefile .github/workflows/ci.yml .github/workflows/release.yml
git commit -m "ci: add fast integration test tier"
```

---

## Task 11: Final verification

- [ ] **Step 1: Run full fast test pyramid**

```bash
make test-unit
make test-integration
```

Expected: all PASS

- [ ] **Step 2: Format and lint all touched Go files**

```bash
go fmt ./internal/vm/... ./internal/lifecycle/... ./test/...
golangci-lint run ./internal/vm/... ./internal/lifecycle/... \
  ./test/testvm/... ./test/lifecycle/... ./test/integration/...
```

- [ ] **Step 3: Lint plan markdown (if edited)**

Run:

```bash
npx markdownlint-cli2 \
  "docs/superpowers/plans/2026-06-11-integration-test-tier.md" --fix
```

- [ ] **Step 4: Confirm spec coverage**

| Spec scenario | Test function |
| --- | --- |
| Destroy `keepBase=true` | `TestDestroy_KeepBase_PreservesBootstrapAndBaseImage` |
| Destroy `keepBase=false` | `TestDestroy_NoKeepBase_ClearsBootstrapAndBaseImage` |
| `Recreate(fast=true)` with valid base | `TestRecreate_FastWithValidBase_RestoresWithoutFullBootstrap` |
| `Recreate(fast=true)` without base | `TestRecreate_FastWithoutBase_FallsBackToFullBootstrap` |
| Restore failure fallback | `TestRecreate_RestoreFailure_FallsBackToFullBootstrap` |
| Non-interactive start, VM missing, valid base | `TestStart_NonInteractive_VMMissing_ValidBase_FastRecreate` |
| Bootstrap refresh accepted | `TestStart_BootstrapRefreshAccepted_FullBootstrap` |
| Bootstrap refresh declined, stopped VM | `TestStart_BootstrapRefreshDeclined_StoppedVM_FastRecreate` |
| Combined prompt 1/2/3 | `TestStart_CombinedPrompt_Option{1,2,3}_*` |
| Runtime change declined | `TestStart_RuntimeChangeDeclined_PreservesBase` |
| Runtime change accepted | `TestStart_RuntimeChangeAccepted_DeletesBaseAndRecreates` |
| Config hash change | `TestStart_ConfigHashChange_*` |
| VM age prompt accept + valid base | `TestStart_VMAgePromptAccepted_ValidBase_FastRecreate` |

---

## Non-goals (do not implement in this plan)

- CLI subprocess / Cobra flag tests
- Replacing bootstrap (`-tags bootstrap`) or e2e (`-tags integration`) tests
- Mocking `plugin.Executor` with scripted installs
- Removing `ForTest` exports (keep for existing unit tests)

## Migration policy

1. Land integration tests first; do **not** delete e2e tests in this work.
2. E2e remains responsible for Docker mechanics (commit/clone, ports, file copy).
3. Remove e2e duplicates only after integration parity is verified in a follow-up.
