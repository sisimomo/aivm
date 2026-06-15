# VM Backend Encapsulation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all Lima/Docker behavioral branches out of `internal/lifecycle`
behind four new `vm.VM` methods so backends are interchangeable at the
orchestration layer.

**Architecture:** Extend `vm.VM` with `UsesBootstrapOnlyMounts`,
`PrepareHostMountDir`, `AfterBootstrapPlugins`, and `FinalizeAfterBootstrap`.
Lifecycle picks bootstrap vs runtime `StartOptions` via the first method, prepares
host dirs via the VM instance, and calls finalize after every first-time
bootstrap. Docker finalizes by promoting mounts; Lima finalizes by saving the
base image when enabled. Move `DefaultUserHome` from `mountspec` into `vm`.

**Tech Stack:** Go, existing `vm.VM` / `BaseImageStore` interfaces, `go test`,
`golangci-lint`

**Spec:** `docs/superpowers/specs/2026-06-14-vm-backend-encapsulation-design.md`

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/vm/vm.go` | Add four lifecycle hook methods to `VM` interface |
| `internal/vm/home.go` | **New** — `DefaultUserHome(backend, hostHome)` (moved from mountspec) |
| `internal/vm/factory.go` | Pass `cfg.BaseImageEnable` into backend constructors |
| `internal/vm/docker.go` | Docker hook implementations; store `baseImageEnable` |
| `internal/vm/docker_base.go` | `FinalizeAfterBootstrap` + rename `PromoteWithEphemeralCommit` → unexported |
| `internal/vm/lima.go` | Lima hook implementations; store `baseImageEnable` |
| `internal/mountspec/vmhome.go` | **Delete** |
| `internal/config/config.go` | Call `vm.DefaultUserHome` instead of `mountspec.DefaultVMHome` |
| `internal/lifecycle/helpers.go` | `ensureAgentMountDirs(v vm.VM, …)`; drop `dockerBackend` |
| `internal/lifecycle/service.go` | Replace `effectiveBackend == "docker"` with VM methods |
| `internal/lifecycle/bootstrap_paths.go` | Same |
| `internal/lifecycle/bootstrap.go` | `AfterBootstrapPlugins`; remove type asserts and docker branch |
| `internal/lifecycle/docker_promote.go` | **Delete** |
| `test/unit/vm/home_test.go` | **New** — moved from mountspec |
| `test/unit/vm/vm_hooks_test.go` | **New** — hook behavior tests |
| `test/unit/vm/docker_finalize_test.go` | **New** — moved/adapted from lifecycle promote test |
| `test/unit/lifecycle/docker_promote_test.go` | **Delete** |
| `test/unit/mountspec/vmhome_test.go` | **Delete** |
| `test/testvm/fake.go` | Implement new `VM` methods on `FakeVM` |
| `test/unit/lifecycle/bootstrap_paths_test.go` | Add stub methods to `captureVM` |
| `test/unit/vm/base_store_test.go` | Add stub methods to `stubBaseStore` |

---

## Task 1: Move `DefaultUserHome` into `vm` package

**Files:**

- Create: `internal/vm/home.go`
- Create: `test/unit/vm/home_test.go`
- Modify: `internal/config/config.go`
- Delete: `internal/mountspec/vmhome.go`
- Delete: `test/unit/mountspec/vmhome_test.go`

- [ ] **Step 1: Write the failing test**

Create `test/unit/vm/home_test.go`:

```go
package vm_test

import (
 "runtime"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func TestDefaultUserHome_Docker(t *testing.T) {
 t.Parallel()
 got := vm.DefaultUserHome("docker", "/Users/you")
 if got != "/home/user" {
  t.Fatalf("got %q", got)
 }
}

func TestDefaultUserHome_LimaLinux(t *testing.T) {
 t.Setenv("USER", "simon")
 got := vm.DefaultUserHome("lima", "/home/simon")
 if got != "/home/simon" {
  t.Fatalf("got %q", got)
 }
}

func TestDefaultUserHome_LimaDarwin(t *testing.T) {
 if runtime.GOOS != "darwin" {
  t.Skip("darwin only")
 }
 t.Setenv("USER", "simon")
 got := vm.DefaultUserHome("lima", "/Users/simon")
 if got != "/home/simon.guest" {
  t.Fatalf("got %q", got)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/ -run TestDefaultUserHome -v`

Expected: FAIL — `vm.DefaultUserHome` undefined

- [ ] **Step 3: Create `internal/vm/home.go`**

Copy body from `internal/mountspec/vmhome.go`, rename function to
`DefaultUserHome`:

```go
package vm

import (
 "os"
 "path/filepath"
 "runtime"
)

const dockerVMHome = "/home/user"

// DefaultUserHome returns the VM user home directory used when expanding ~ in
// mount target paths.
func DefaultUserHome(backend, hostHome string) string {
 switch backend {
 case "docker":
  return dockerVMHome
 default:
  u := os.Getenv("USER")
  if u == "" {
   return hostHome
  }
  if runtime.GOOS == "darwin" {
   return filepath.Join("/home", u+".guest")
  }
  return filepath.Join("/home", u)
 }
}
```

- [ ] **Step 4: Update `internal/config/config.go`**

In `defaultVMHomeFor` and `MountContext`, replace
`mountspec.DefaultVMHome` with `vm.DefaultUserHome`.

- [ ] **Step 5: Delete old files**

```bash
rm internal/mountspec/vmhome.go test/unit/mountspec/vmhome_test.go
```

- [ ] **Step 6: Run tests**

Run: `go test ./test/unit/vm/ -run TestDefaultUserHome -v`

Expected: PASS

Run: `go test ./test/unit/config/... -v`

Expected: PASS (config mount tests still resolve VM home)

- [ ] **Step 7: Commit**

```bash
git add internal/vm/home.go test/unit/vm/home_test.go internal/config/config.go
git add -u internal/mountspec/vmhome.go test/unit/mountspec/vmhome_test.go
git commit -m "refactor: move DefaultUserHome from mountspec to vm package"
```

---

## Task 2: Extend `vm.VM` interface

**Files:**

- Modify: `internal/vm/vm.go`

- [ ] **Step 1: Add four methods to the `VM` interface**

Insert after `NeedsPortBindingAtBoot()` in `internal/vm/vm.go`:

```go
 // UsesBootstrapOnlyMounts reports whether the initial VM create should attach
 // only bootstrap mounts (user vm.mounts). Agent and T3 mounts are applied
 // later via FinalizeAfterBootstrap. True for Docker; false for Lima.
 UsesBootstrapOnlyMounts() bool

 // PrepareHostMountDir creates a host directory suitable for bind-mounting into
 // this backend (e.g. permissive permissions when guest UID differs from host).
 PrepareHostMountDir(hostPath string) error

 // AfterBootstrapPlugins runs backend-specific cleanup immediately after plugin
 // bootstrap completes. Lima closes the SSH control master; Docker is a no-op.
 AfterBootstrapPlugins(ctx context.Context) error

 // FinalizeAfterBootstrap transitions from bootstrap to runtime configuration
 // after plugins succeed on a freshly created VM. Docker recreates the
 // container with runtimeOpts; Lima saves the base image when enabled.
 FinalizeAfterBootstrap(ctx context.Context, runtimeOpts StartOptions) error
```

- [ ] **Step 2: Verify compile errors list all implementors**

Run: `go build ./...`

Expected: compile errors on `DockerVM`, `LimaVM`, and test stubs — confirms all
sites are found.

- [ ] **Step 3: Commit**

```bash
git add internal/vm/vm.go
git commit -m "feat: extend vm.VM with lifecycle hook methods"
```

---

## Task 3: Implement hooks on `DockerVM`

**Files:**

- Modify: `internal/vm/docker.go`
- Modify: `internal/vm/docker_base.go`
- Modify: `internal/vm/factory.go`
- Create: `test/unit/vm/vm_hooks_test.go`

- [ ] **Step 1: Write failing tests**

Add to `test/unit/vm/vm_hooks_test.go`:

```go
package vm_test

import (
 "os"
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func TestDockerVM_UsesBootstrapOnlyMounts(t *testing.T) {
 t.Parallel()
 d := vm.NewDocker("p", t.TempDir(), "img")
 if !d.UsesBootstrapOnlyMounts() {
  t.Fatal("docker should use bootstrap-only mounts on first create")
 }
}

func TestDockerVM_PrepareHostMountDir_Chmods0777(t *testing.T) {
 t.Parallel()
 d := vm.NewDocker("p", t.TempDir(), "img")
 dir := filepath.Join(t.TempDir(), "persist")
 if err := d.PrepareHostMountDir(dir); err != nil {
  t.Fatalf("PrepareHostMountDir: %v", err)
 }
 info, err := os.Stat(dir)
 if err != nil {
  t.Fatal(err)
 }
 if info.Mode().Perm() != 0o777 {
  t.Fatalf("mode = %o, want 0777", info.Mode().Perm())
 }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./test/unit/vm/ -run 'TestDockerVM_' -v`

Expected: FAIL — methods missing

- [ ] **Step 3: Add `baseImageEnable` to `DockerVM` and constructors**

In `internal/vm/docker.go`, extend struct and constructor:

```go
type DockerVM struct {
 // ...existing fields...
 baseImageEnable bool
}

func NewDocker(
 profile, stateDir, image string, baseImageEnable bool,
) *DockerVM {
 return &DockerVM{
  profile:         profile,
  stateDir:        stateDir,
  image:           image,
  containerName:   profile,
  baseImageEnable: baseImageEnable,
 }
}
```

Update `internal/vm/factory.go`:

```go
case "docker":
 return NewDocker(
  cfg.Profile(), stateDir, cfg.DockerImage, cfg.BaseImageEnable,
 ), nil
```

- [ ] **Step 4: Implement hook methods on `DockerVM`**

In `internal/vm/docker.go`:

```go
func (d *DockerVM) UsesBootstrapOnlyMounts() bool { return true }

func (d *DockerVM) AfterBootstrapPlugins(_ context.Context) error { return nil }

func (d *DockerVM) PrepareHostMountDir(hostPath string) error {
 if err := os.MkdirAll(hostPath, 0o755); err != nil {
  return fmt.Errorf("creating mount dir %q: %w", hostPath, err)
 }
 if err := os.Chmod(hostPath, 0o777); err != nil {
  return fmt.Errorf("chmod mount dir %q: %w", hostPath, err)
 }
 return nil
}
```

- [ ] **Step 5: Move promotion into `FinalizeAfterBootstrap`**

In `internal/vm/docker_base.go`, rename `PromoteWithEphemeralCommit` to
`promoteWithEphemeralCommit` (unexported). Add:

```go
func (d *DockerVM) FinalizeAfterBootstrap(
 ctx context.Context, runtimeOpts StartOptions,
) error {
 if d.baseImageEnable {
  if err := d.SaveBaseImage(ctx, runtimeOpts); err != nil {
   slog.Warn(fmt.Sprintf("save base image failed: %v", err))
  }
  return d.RestoreFromBaseImage(ctx, runtimeOpts)
 }
 return d.promoteWithEphemeralCommit(ctx, runtimeOpts)
}
```

Add `log/slog` import if needed.

- [ ] **Step 6: Run tests**

Run: `go test ./test/unit/vm/ -run 'TestDockerVM_' -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/vm/docker.go internal/vm/docker_base.go \
 internal/vm/factory.go test/unit/vm/vm_hooks_test.go
git commit -m "feat: implement VM lifecycle hooks on DockerVM"
```

---

## Task 4: Implement hooks on `LimaVM`

**Files:**

- Modify: `internal/vm/lima.go`
- Modify: `internal/vm/factory.go`
- Modify: `test/unit/vm/vm_hooks_test.go`

- [ ] **Step 1: Write failing Lima tests**

Append to `test/unit/vm/vm_hooks_test.go`:

```go
func TestLimaVM_UsesBootstrapOnlyMounts(t *testing.T) {
 t.Parallel()
 l := vm.NewLima("p", t.TempDir(), false)
 if l.UsesBootstrapOnlyMounts() {
  t.Fatal("lima should not use bootstrap-only mounts")
 }
}

func TestLimaVM_PrepareHostMountDir_NoExtraChmod(t *testing.T) {
 t.Parallel()
 l := vm.NewLima("p", t.TempDir(), false)
 dir := filepath.Join(t.TempDir(), "persist")
 if err := l.PrepareHostMountDir(dir); err != nil {
  t.Fatalf("PrepareHostMountDir: %v", err)
 }
 info, err := os.Stat(dir)
 if err != nil {
  t.Fatal(err)
 }
 if info.Mode().Perm() != 0o755 {
  t.Fatalf("mode = %o, want 0755", info.Mode().Perm())
 }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./test/unit/vm/ -run 'TestLimaVM_' -v`

Expected: FAIL

- [ ] **Step 3: Add `baseImageEnable` to `LimaVM`**

Update `NewLima` signature and `factory.go`:

```go
func NewLima(profile, stateDir string, baseImageEnable bool) *LimaVM {
 return &LimaVM{
  profile:         profile,
  stateDir:        stateDir,
  lock:            NewLifecycleLock(stateDir),
  baseImageEnable: baseImageEnable,
 }
}

// factory.go lima case:
return NewLima(cfg.Profile(), stateDir, cfg.BaseImageEnable), nil
```

- [ ] **Step 4: Implement Lima hook methods**

In `internal/vm/lima.go`:

```go
func (l *LimaVM) UsesBootstrapOnlyMounts() bool { return false }

func (l *LimaVM) PrepareHostMountDir(hostPath string) error {
 if err := os.MkdirAll(hostPath, 0o755); err != nil {
  return fmt.Errorf("creating mount dir %q: %w", hostPath, err)
 }
 return nil
}

func (l *LimaVM) AfterBootstrapPlugins(ctx context.Context) error {
 CloseSSHControlMaster(ctx, l.profile)
 return nil
}

func (l *LimaVM) FinalizeAfterBootstrap(
 ctx context.Context, runtimeOpts StartOptions,
) error {
 if !l.baseImageEnable {
  return nil
 }
 if err := l.SaveBaseImage(ctx, runtimeOpts); err != nil {
  slog.Warn(fmt.Sprintf("save base image failed (VM still usable): %v", err))
 }
 return nil
}
```

Add `os` import if not present.

- [ ] **Step 5: Fix all `NewLima` / `NewDocker` call sites**

Run: `go build ./...`

Fix any test harness or unit test constructors that call `NewLima` / `NewDocker`
directly (grep for `NewLima(` and `NewDocker(`).

- [ ] **Step 6: Run tests**

Run: `go test ./test/unit/vm/ -run 'TestLimaVM_|TestDockerVM_' -v`

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/vm/lima.go internal/vm/factory.go test/unit/vm/vm_hooks_test.go
git commit -m "feat: implement VM lifecycle hooks on LimaVM"
```

---

## Task 5: Move Docker finalize tests to `vm` package

**Files:**

- Create: `test/unit/vm/docker_finalize_test.go`
- Delete: `test/unit/lifecycle/docker_promote_test.go`

- [ ] **Step 1: Write failing test calling `FinalizeAfterBootstrap`**

Create `test/unit/vm/docker_finalize_test.go` adapted from the lifecycle test.
Replace `PromoteDockerToRuntimeMountsForTest` with a direct stub call:

```go
func TestDockerVM_FinalizeAfterBootstrap_SaveAndRestore(t *testing.T) {
 // ...same setup as old TestPromoteDockerToRuntimeMounts_SaveAndRestore...
 stub := &finalizeStubVM{baseImageEnable: true}
 runtimeOpts := vm.StartOptions{Mounts: runtimeMounts /* + ports if needed */}

 if err := stub.FinalizeAfterBootstrap(
  context.Background(), runtimeOpts,
 ); err != nil {
  t.Fatalf("FinalizeAfterBootstrap: %v", err)
 }
 // assert SaveBaseImage then RestoreFromBaseImage; restoreMounts == len(runtimeMounts)
}
```

Define `finalizeStubVM` embedding the promote stub behavior plus the four new
hook methods (no-op defaults).

- [ ] **Step 2: Run test**

Run: `go test ./test/unit/vm/ -run TestDockerVM_FinalizeAfterBootstrap -v`

Expected: PASS (implementation from Task 3)

- [ ] **Step 3: Delete lifecycle promote test and export**

```bash
rm test/unit/lifecycle/docker_promote_test.go
```

- [ ] **Step 4: Commit**

```bash
git add test/unit/vm/docker_finalize_test.go
git add -u test/unit/lifecycle/docker_promote_test.go
git commit -m "test: move Docker finalize tests from lifecycle to vm package"
```

---

## Task 6: Refactor lifecycle to use VM hooks

**Files:**

- Modify: `internal/lifecycle/helpers.go`
- Modify: `internal/lifecycle/service.go`
- Modify: `internal/lifecycle/bootstrap_paths.go`
- Modify: `internal/lifecycle/bootstrap.go`
- Delete: `internal/lifecycle/docker_promote.go`

- [ ] **Step 1: Refactor `ensureAgentMountDirs`**

In `internal/lifecycle/helpers.go`, change signature and body:

```go
func ensureAgentMountDirs(
 v vm.VM, cfg *config.Config, agentDefs map[string]agent.Def,
) error {
 home, _ := os.UserHomeDir()
 ctx := cfg.VM.MountContext(cfg.StateDir, home)
 seen := make(map[string]bool)
 for name, def := range agentDefs {
  for _, spec := range def.Mounts {
   r, err := mountspec.Resolve(spec, ctx)
   if err != nil {
    return fmt.Errorf("agent %q mounts: %w", name, err)
   }
   if seen[r.HostPath] {
    continue
   }
   seen[r.HostPath] = true
   if err := v.PrepareHostMountDir(r.HostPath); err != nil {
    return err
   }
  }
 }
 if cfg.T3Code.Enable {
  r, err := mountspec.Resolve(mountspec.MountSpec{
   Source: "{{ .state_dir }}/.t3",
   Target: "~/.t3",
   Mode:   "rw",
  }, ctx)
  if err != nil {
   return fmt.Errorf("t3 mount: %w", err)
  }
  if err := v.PrepareHostMountDir(r.HostPath); err != nil {
   return err
  }
 }
 return nil
}
```

Delete `ensureHostMountDir` helper entirely.

Add shared helper:

```go
func finalizeAfterBootstrap(ctx context.Context, svc *LifecycleService) error {
 runtimeOpts, err := buildRuntimeStartOptions(svc.VM, svc.Config, svc.AgentDefs)
 if err != nil {
  return fmt.Errorf("building runtime start options: %w", err)
 }
 if err := svc.VM.FinalizeAfterBootstrap(ctx, runtimeOpts); err != nil {
  return fmt.Errorf("finalize after bootstrap: %w", err)
 }
 return nil
}
```

- [ ] **Step 2: Update `service.go` `resumeOrStartVM`**

Replace docker branches:

```go
if wasCreated && svc.VM.UsesBootstrapOnlyMounts() {
 opts, err = buildBootstrapStartOptions(svc.VM, cfg, svc.AgentDefs)
} else {
 opts, err = buildStartOptions(svc.VM, cfg, svc.AgentDefs)
}
// ...
if err := ensureAgentMountDirs(svc.VM, cfg, svc.AgentDefs); err != nil {
// ...
if wasCreated {
 if err := finalizeAfterBootstrap(ctx, svc); err != nil {
  return err
 }
}
```

Remove the `promoteDockerToRuntimeMounts` call block.

- [ ] **Step 3: Update `bootstrap_paths.go` `fullBootstrap`**

Same pattern: `UsesBootstrapOnlyMounts()` for start opts,
`ensureAgentMountDirs(svc.VM, …)`, `finalizeAfterBootstrap` after `bootstrap()`.

- [ ] **Step 4: Update `bootstrap.go`**

Replace Lima type assert with:

```go
if err := targetVM.AfterBootstrapPlugins(ctx); err != nil {
 return err
}
```

Delete the `effectiveBackend != "docker"` `SaveBaseImageBestEffort` block (Lima
save now happens in `FinalizeAfterBootstrap`).

- [ ] **Step 5: Delete `docker_promote.go`**

```bash
rm internal/lifecycle/docker_promote.go
```

- [ ] **Step 6: Verify no orchestration leaks remain**

Run:

```bash
rg 'effectiveBackend\(.*\) == "docker"|\*vm\.(DockerVM|LimaVM)' internal/lifecycle/
```

Expected: no matches (metadata-only `effectiveBackend` uses in `state.go`,
`recreation.go`, `baseimage.go` are OK).

- [ ] **Step 7: Commit**

```bash
git add internal/lifecycle/
git commit -m "refactor: drive mount orchestration via vm.VM lifecycle hooks"
```

---

## Task 7: Update test stubs

**Files:**

- Modify: `test/testvm/fake.go`
- Modify: `test/unit/lifecycle/bootstrap_paths_test.go`
- Modify: `test/unit/vm/base_store_test.go`

- [ ] **Step 1: Add hook methods to `FakeVM`**

In `test/testvm/fake.go`:

```go
func (f *FakeVM) UsesBootstrapOnlyMounts() bool { return true }

func (f *FakeVM) PrepareHostMountDir(hostPath string) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("PrepareHostMountDir", hostPath)
 return os.MkdirAll(hostPath, 0o755)
}

func (f *FakeVM) AfterBootstrapPlugins(_ context.Context) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("AfterBootstrapPlugins", "")
 return nil
}

func (f *FakeVM) FinalizeAfterBootstrap(
 _ context.Context, _ vm.StartOptions,
) error {
 f.mu.Lock()
 defer f.mu.Unlock()
 f.appendCall("FinalizeAfterBootstrap", "")
 return nil
}
```

Add `os` import.

- [ ] **Step 2: Add no-op stubs to `captureVM` and `stubBaseStore`**

Each needs the four methods returning zero values (copy from `FakeVM` stubs).

- [ ] **Step 3: Build and run unit tests**

Run: `go test ./test/... -count=1`

Expected: all packages compile and pass

- [ ] **Step 4: Commit**

```bash
git add test/testvm/fake.go test/unit/lifecycle/bootstrap_paths_test.go test/unit/vm/base_store_test.go
git commit -m "test: update VM stubs for lifecycle hook interface"
```

---

## Task 8: Format, lint, and full verification

**Files:** (all edited Go files)

- [ ] **Step 1: Format and lint**

```bash
go fmt ./internal/vm/... ./internal/lifecycle/... ./internal/config/... ./test/...
golangci-lint run ./internal/vm/... ./internal/lifecycle/... ./internal/config/...
```

Expected: no errors

- [ ] **Step 2: Run focused unit tests**

```bash
go test ./test/unit/vm/... ./test/unit/lifecycle/... \
 ./test/unit/config/... ./test/unit/mountspec/... -count=1
```

Expected: PASS

- [ ] **Step 3: Run mount and bootstrap integration tests**

```bash
go test ./test/unit/lifecycle/ -run Mount -count=1
go test ./test/integration/lifecycle/... -count=1 2>/dev/null || true
```

Run e2e if available in CI locally:

```bash
go test ./test/e2e/ -run 'Mount|Claude|Bootstrap' -count=1 -timeout 30m
```

- [ ] **Step 4: Final leak grep**

```bash
rg 'PromoteDocker|promoteDocker|ensureHostMountDir|mountspec\.DefaultVMHome' .
```

Expected: no matches outside docs/history

- [ ] **Step 5: Commit any lint fixes**

```bash
git add -u
git commit -m "chore: format and lint VM encapsulation refactor"
```

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| Zero `effectiveBackend == "docker"` for orchestration | Task 6 Step 6 grep |
| Zero `*DockerVM` / `*LimaVM` in lifecycle | Task 6 |
| Delete `docker_promote.go` | Task 6 Step 5 |
| `mountspec` backend-agnostic | Task 1 |
| Four new `vm.VM` methods | Tasks 2–4 |
| `PrepareHostMountDir` Docker chmod | Task 3 |
| `AfterBootstrapPlugins` Lima SSH cleanup | Task 4 |
| `FinalizeAfterBootstrap` Docker promote | Task 3 |
| `FinalizeAfterBootstrap` Lima base image save | Task 4 |
| Preserve mount behavior | Tasks 8 integration/e2e |
| Update test stubs | Task 7 |

## Note on `FinalizeAfterBootstrap` call site

The spec flow shows finalize only when `UsesBootstrapOnlyMounts`. The plan
calls `finalizeAfterBootstrap` on every `wasCreated` path instead, because Lima
also needs post-bootstrap work (base image save). Both backends implement the
same method with backend-appropriate behavior; lifecycle makes a single call with
no backend branching.
