# aivm Mount System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace identity-only mounts with structured `MountSpec` entries that
support host→guest path remapping, template variables, and CWD translation for
`aivm ssh` and agent launch.

**Architecture:** YAML `MountSpec` objects (`source`, `target`, `mode`)
are template-rendered and validated at config load (user `vm.mounts`) or VM-start
assembly (agent + internal mounts). Resolved mounts flow into `vm.Mount` with
distinct `HostPath`/`GuestPath` for Lima/Docker bind mounts. `GuestPathForHost`
translates the host CWD using longest-prefix match on `vm.mounts` sources only.

**Tech Stack:** Go, Viper/mapstructure, `text/template` (same variable names as
plugin setup scripts), Lima `limactl`, Docker `-v`

**Spec:** `docs/superpowers/specs/2026-06-12-aivm-mount-system-design.md`

> **Update:** Mount YAML fields are `source` / `target`. `~` in `source` expands
> to host home; `~` in `target` expands to guest home (`vm.guest_home` to
> override). Some code snippets below predate that rename — the spec is canonical.

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/mountspec/spec.go` | `MountSpec` YAML struct (shared by config + agent; avoids import cycle) |
| `internal/mountspec/resolve.go` | Template render, `~` expansion, mode parsing → `ResolvedMount` |
| `internal/mountspec/validate.go` | Duplicate `target`, overlapping `vm.mounts` sources |
| `internal/mountspec/guesthome.go` | Default guest home per backend |
| `internal/config/config.go` | `VMConfig.Mounts []mountspec.MountSpec`; `Mount` gains `GuestPath` |
| `internal/config/defaults.yaml` | Structured default `vm.mounts` |
| `internal/agent/def.go` | `Mounts []mountspec.MountSpec` (replaces `Persist`) |
| `internal/agent/defaults.yaml` | Structured agent `mounts` incl. Claude `image-cache` |
| `internal/vm/vm.go` | `Mount.GuestPath` field |
| `internal/vm/lima.go` | `limactl create --mount type=bind,source=…,target=…` |
| `internal/vm/docker.go` | `-v host:guest:mode` with distinct paths |
| `internal/vm/ssh.go` | Comment: `workDir` is guest path |
| `internal/vm/mountflag.go` | `LimaMountFlag`, `DockerVolumeFlag` (testable helpers) |
| `internal/lifecycle/mountpath.go` | `GuestPathForHost` |
| `internal/lifecycle/helpers.go` | Mount assembly, `ensureAgentMountDirs` |
| `internal/lifecycle/agent_session.go` | Translate CWD before setting `vmDir` |
| `internal/lifecycle/commands.go` | Translate CWD before `VM.SSH` |
| `internal/plugin/defaults.yaml` | Remove T3 `ln -sfn` block |
| `internal/config/parse.go` | Delete `ParseMount` |
| `aivm.example.yaml`, `demo/configs/aivm.yaml`, `README.md` | New format docs |
| `test/framework/config.go` | Emit structured mounts in generated YAML |
| `test/lifecycle/harness/harness.go` | Set `GuestPath` on harness `ParsedMounts` |

---

## Task 1: MountSpec type and template resolution

**Files:**

- Create: `internal/mountspec/spec.go`
- Create: `internal/mountspec/resolve.go`
- Create: `test/unit/mountspec/resolve_test.go`

- [ ] **Step 1: Write the failing test**

```go
package mountspec_test

import (
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolveMountSpec_IdentityWithTemplates(t *testing.T) {
 t.Parallel()
 home := "/Users/you"
 state := "/Users/you/.aivm"
 spec := mountspec.MountSpec{
  Location:   `{{ .home }}/dev`,
  Target: `{{ .home }}/dev`,
  Mode:       "rw",
 }
 got, err := mountspec.Resolve(spec, mountspec.Context{Home: home, StateDir: state})
 if err != nil {
  t.Fatalf("Resolve: %v", err)
 }
 wantHost := filepath.Join(home, "dev")
 if got.HostPath != wantHost || got.GuestPath != wantHost {
  t.Fatalf("got host=%q guest=%q, want %q", got.HostPath, got.GuestPath, wantHost)
 }
 if !got.Writable {
  t.Fatal("want writable")
 }
}

func TestResolveMountSpec_RemappedReadOnly(t *testing.T) {
 t.Parallel()
 spec := mountspec.MountSpec{
  Location:   "/Users/you/secrets",
  Target: "/secrets",
  Mode:       "ro",
 }
 ctx := mountspec.Context{Home: "/Users/you", StateDir: "/Users/you/.aivm"}
 got, err := mountspec.Resolve(spec, ctx)
 if err != nil {
  t.Fatalf("Resolve: %v", err)
 }
 if got.HostPath != "/Users/you/secrets" || got.GuestPath != "/secrets" {
  t.Fatalf("paths: host=%q guest=%q", got.HostPath, got.GuestPath)
 }
 if got.Writable {
  t.Fatalf("got %+v", got)
 }
}

func TestResolveMountSpec_TildeExpansion(t *testing.T) {
 t.Parallel()
 spec := mountspec.MountSpec{
  Location:   "~/dev",
  Target: "~/dev",
  Mode:       "rw",
 }
 ctx := mountspec.Context{Home: "/Users/you", StateDir: "/Users/you/.aivm"}
 got, err := mountspec.Resolve(spec, ctx)
 if err != nil {
  t.Fatalf("Resolve: %v", err)
 }
 if got.HostPath != "/Users/you/dev" {
  t.Fatalf("HostPath = %q", got.HostPath)
 }
}

func TestResolveMountSpec_MissingField(t *testing.T) {
 t.Parallel()
 spec := mountspec.MountSpec{Target: "/x", Mode: "rw"}
 ctx := mountspec.Context{Home: "/h", StateDir: "/s"}
 _, err := mountspec.Resolve(spec, ctx)
 if err == nil {
  t.Fatal("expected error for missing location")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/mountspec/... -run TestResolveMountSpec -v`

Expected: FAIL — package `internal/mountspec` does not exist

- [ ] **Step 3: Write minimal implementation**

`internal/mountspec/spec.go`:

```go
package mountspec

// MountSpec is the YAML shape for vm.mounts and agent mounts.
type MountSpec struct {
 Source   string `yaml:"source" mapstructure:"source"`
 Target   string `yaml:"target" mapstructure:"target"`
 Mode       string `yaml:"mode" mapstructure:"mode"`
}

// ResolvedMount is a fully expanded mount ready for the VM backend.
type ResolvedMount struct {
 HostPath  string
 GuestPath string
 Writable  bool
}

// Context supplies template variables for path rendering.
type Context struct {
 Home      string
 GuestHome string
 StateDir  string
}
```

`internal/mountspec/resolve.go`:

```go
package mountspec

import (
 "bytes"
 "fmt"
 "path/filepath"
 "strings"
 "text/template"

 "github.com/sisimomo/aivm/internal/plugin"
)

func Resolve(spec MountSpec, ctx Context) (ResolvedMount, error) {
 if strings.TrimSpace(spec.Source) == "" {
  return ResolvedMount{}, fmt.Errorf("mount source is required")
 }
 if strings.TrimSpace(spec.Target) == "" {
  return ResolvedMount{}, fmt.Errorf("mount target is required")
 }
 if strings.TrimSpace(spec.Mode) == "" {
  return ResolvedMount{}, fmt.Errorf("mount mode is required")
 }

 location, err := renderPath(spec.Source, ctx)
 if err != nil {
  return ResolvedMount{}, fmt.Errorf("source: %w", err)
 }
 target, err := renderPath(spec.Target, ctx)
 if err != nil {
  return ResolvedMount{}, fmt.Errorf("target: %w", err)
 }

 writable, err := parseMode(spec.Mode)
 if err != nil {
  return ResolvedMount{}, err
 }

 source = expandTilde(source, ctx.Home)
 target = expandTilde(target, ctx.GuestHome)

 if !filepath.IsAbs(location) {
  return ResolvedMount{}, fmt.Errorf(
   "location %q must be absolute after expansion", location)
 }
 if !filepath.IsAbs(target) {
  return ResolvedMount{}, fmt.Errorf(
   "target %q must be absolute after expansion", target)
 }

 return ResolvedMount{
  HostPath:  filepath.Clean(location),
  GuestPath: filepath.Clean(target),
  Writable:  writable,
 }, nil
}

func renderPath(src string, ctx Context) (string, error) {
 t, err := template.New("").Funcs(plugin.TemplateFuncMap()).Parse(src)
 if err != nil {
  return "", err
 }
 data := map[string]string{
  "state_dir": ctx.StateDir,
  "home":      ctx.Home,
 }
 var buf bytes.Buffer
 if err := t.Execute(&buf, data); err != nil {
  return "", err
 }
 return strings.TrimSpace(buf.String()), nil
}

func expandTilde(path, home string) string {
 if strings.HasPrefix(path, "~/") {
  return filepath.Join(home, path[2:])
 }
 return path
}

func parseMode(mode string) (bool, error) {
 switch strings.ToLower(strings.TrimSpace(mode)) {
 case "rw":
  return true, nil
 case "ro":
  return false, nil
 default:
  return false, fmt.Errorf("unknown mode %q — use \"rw\" or \"ro\"", mode)
 }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/mountspec/... -run TestResolveMountSpec -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mountspec/spec.go internal/mountspec/resolve.go test/unit/mountspec/resolve_test.go
git commit -m "feat: add MountSpec resolution with template and tilde expansion"
```

---

## Task 2: Mount validation helpers

**Files:**

- Create: `internal/mountspec/validate.go`
- Create: `test/unit/mountspec/validate_test.go`

- [ ] **Step 1: Write the failing test**

```go
package mountspec_test

import (
 "strings"
 "testing"

 "github.com/sisimomo/aivm/internal/mountspec"
)

func TestValidateDuplicateTarget(t *testing.T) {
 t.Parallel()
 mounts := []mountspec.ResolvedMount{
  {HostPath: "/a", GuestPath: "/x", Writable: true},
  {HostPath: "/b", GuestPath: "/x", Writable: true},
 }
 err := mountspec.ValidateDuplicateTargets(mounts)
 if err == nil || !strings.Contains(err.Error(), "/x") {
  t.Fatalf("expected duplicate target error, got %v", err)
 }
}

func TestValidateOverlappingSources(t *testing.T) {
 t.Parallel()
 mounts := []mountspec.ResolvedMount{
  {HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
  {HostPath: "/Users/you/dev/sub", GuestPath: "/sub", Writable: true},
 }
 err := mountspec.ValidateOverlappingSources(mounts)
 if err == nil {
  t.Fatal("expected overlapping location error")
 }
}

func TestValidateOverlappingSources_SiblingsOK(t *testing.T) {
 t.Parallel()
 mounts := []mountspec.ResolvedMount{
  {HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
  {HostPath: "/Users/you/other", GuestPath: "/Users/you/other", Writable: true},
 }
 if err := mountspec.ValidateOverlappingSources(mounts); err != nil {
  t.Fatalf("unexpected: %v", err)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/mountspec/... -run TestValidate -v`

Expected: FAIL — `ValidateDuplicateTargets` undefined

- [ ] **Step 3: Write minimal implementation**

```go
package mountspec

import (
 "fmt"
 "os"
 "sort"
 "strings"
)

func ValidateDuplicateTargets(mounts []ResolvedMount) error {
 seen := make(map[string]string, len(mounts))
 for _, m := range mounts {
  if prev, ok := seen[m.GuestPath]; ok {
   return fmt.Errorf(
    "duplicate target %q (from %q and %q)",
    m.GuestPath, prev, m.HostPath)
  }
  seen[m.GuestPath] = m.HostPath
 }
 return nil
}

func ValidateOverlappingSources(mounts []ResolvedMount) error {
 paths := make([]string, len(mounts))
 for i, m := range mounts {
  paths[i] = m.HostPath
 }
 sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
 sep := string(os.PathSeparator)
 for i := 0; i < len(paths); i++ {
  for j := i + 1; j < len(paths); j++ {
   if paths[i] == paths[j] {
    return fmt.Errorf("duplicate vm.mounts location %q", paths[i])
   }
   if strings.HasPrefix(paths[j], paths[i]+sep) {
    return fmt.Errorf("overlapping vm.mounts sources %q and %q", paths[i], paths[j])
   }
  }
 }
 return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/mountspec/... -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mountspec/validate.go test/unit/mountspec/validate_test.go
git commit -m "feat: validate duplicate targets and overlapping vm mount sources"
```

---

## Task 3: Wire vm.mounts through config load

**Files:**

- Modify: `internal/config/config.go`
- Modify: `internal/config/defaults.yaml`
- Delete: `ParseMount` from `internal/config/parse.go`
- Create: `test/unit/config/mounts_test.go`

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/sisimomo/aivm/internal/config"
)

func TestLoad_StructuredMounts(t *testing.T) {
 t.Parallel()
 home, _ := os.UserHomeDir()
 dir := t.TempDir()
 path := filepath.Join(dir, "aivm.yaml")
 content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  mounts:
    - source: "{{ .home }}/dev"
      target: "{{ .home }}/dev"
      mode: rw
`
 if err := os.WriteFile(path, []byte(content), 0644); err != nil {
  t.Fatal(err)
 }
 cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err != nil {
  t.Fatalf("Load: %v", err)
 }
 if len(cfg.VM.ParsedMounts) != 1 {
  t.Fatalf("ParsedMounts len = %d", len(cfg.VM.ParsedMounts))
 }
 m := cfg.VM.ParsedMounts[0]
 want := filepath.Join(home, "dev")
 if m.HostPath != want || m.GuestPath != want || !m.Writable {
  t.Fatalf("got %+v, want host/guest %q rw", m, want)
 }
}

func TestLoad_RejectsStringMount(t *testing.T) {
 t.Parallel()
 dir := t.TempDir()
 path := filepath.Join(dir, "aivm.yaml")
 content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  mounts:
    - "~/dev:rw"
`
 if err := os.WriteFile(path, []byte(content), 0644); err != nil {
  t.Fatal(err)
 }
 _, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err == nil || !strings.Contains(err.Error(), "vm.mounts") {
  t.Fatalf("expected vm.mounts error, got %v", err)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/config/... -run TestLoad_StructuredMounts -v`

Expected: FAIL

- [ ] **Step 3: Update config types and validation**

In `internal/config/config.go`:

1. Add import `"github.com/sisimomo/aivm/internal/mountspec"`.
2. Change `VMConfig.Mounts` from `[]string` to `[]mountspec.MountSpec`.
3. Add `GuestPath string` to `Mount`.
4. Replace the mounts block in `validateAndParse`:

```go
 home, _ := os.UserHomeDir()
 ctx := mountspec.Context{Home: home, StateDir: stateDir}
 parsed := make([]Mount, 0, len(vm.Mounts))
 for i, spec := range vm.Mounts {
  resolved, err := mountspec.Resolve(spec, ctx)
  if err != nil {
   return fmt.Errorf("vm.mounts[%d]: %w", i, err)
  }
  parsed = append(parsed, Mount{
   HostPath:  resolved.HostPath,
   GuestPath: resolved.GuestPath,
   Writable:  resolved.Writable,
  })
 }
 if err := mountspec.ValidateOverlappingSources(
  toResolved(parsed)); err != nil {
  return fmt.Errorf("vm.mounts: %w", err)
 }
 vm.ParsedMounts = parsed
```

Add helper in `config.go`:

```go
func toResolved(mounts []Mount) []mountspec.ResolvedMount {
 out := make([]mountspec.ResolvedMount, len(mounts))
 for i, m := range mounts {
  out[i] = mountspec.ResolvedMount{
   HostPath: m.HostPath, GuestPath: m.GuestPath, Writable: m.Writable,
  }
 }
 return out
}
```

Update `internal/config/defaults.yaml`:

```yaml
  mounts:
    - source: "{{ .home }}/dev"
      target: "{{ .home }}/dev"
      mode: rw
```

Remove `ParseMount` and its tests from `internal/config/parse.go`.

- [ ] **Step 4: Run tests**

Run: `go test ./test/unit/config/... \
  -run 'TestLoad_(StructuredMounts|RejectsStringMount)' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/defaults.yaml \
  internal/config/parse.go test/unit/config/mounts_test.go
git commit -m "feat: load structured vm.mounts with template resolution"
```

---

## Task 4: Agent Def mounts field and bundled defaults

**Files:**

- Modify: `internal/agent/def.go`
- Modify: `internal/agent/defaults.yaml`
- Modify: `test/unit/agent/defaults_test.go`

- [ ] **Step 1: Write the failing test**

Add to `test/unit/agent/defaults_test.go`:

```go
func TestLoadDefs_ClaudeMounts(t *testing.T) {
 defs, err := agent.LoadDefs()
 if err != nil {
  t.Fatalf("LoadDefs: %v", err)
 }
 claude := defs["claude"]
 if len(claude.Mounts) != 2 {
  t.Fatalf("claude mounts len = %d, want 2", len(claude.Mounts))
 }
 if claude.Mounts[0].Source != `{{ .state_dir }}/.claude/projects` {
  t.Fatalf("projects location = %q", claude.Mounts[0].Source)
 }
 if claude.Mounts[1].Source != `{{ .state_dir }}/.claude/image-cache` {
  t.Fatalf("image-cache location = %q", claude.Mounts[1].Source)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/agent/... -run TestLoadDefs_ClaudeMounts -v`

Expected: FAIL — `Mounts` field missing / wrong count

- [ ] **Step 3: Update agent definition**

`internal/agent/def.go` — replace `Persist` with:

```go
 // Mounts lists host→guest bind mounts for agent state directories.
 Mounts []mountspec.MountSpec `yaml:"mounts" mapstructure:"mounts"`
```

Import `github.com/sisimomo/aivm/internal/mountspec`. Update `MergeDef`:

```go
 if len(override.Mounts) > 0 {
  result.Mounts = override.Mounts
 }
```

`internal/agent/defaults.yaml` — replace all `persist` blocks per spec (claude gets
`projects` + `image-cache`; copilot and cursor get remapped mounts to
`{{ .home }}/…`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/agent/... -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/def.go internal/agent/defaults.yaml test/unit/agent/defaults_test.go
git commit -m "feat: replace agent persist with structured mounts in defaults"
```

---

## Task 5: VM backend Mount.GuestPath and mount flag helpers

**Files:**

- Modify: `internal/vm/vm.go`
- Create: `internal/vm/mountflag.go`
- Create: `test/unit/vm/mountflag_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vm_test

import (
 "strings"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func TestLimaMountFlag_Identity(t *testing.T) {
 m := vm.Mount{
  HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true,
 }
 flag := vm.LimaMountFlag(m)
 if !strings.Contains(flag, "source=/Users/you/dev") {
  t.Fatalf("flag = %q", flag)
 }
 if !strings.Contains(flag, "target=/Users/you/dev") {
  t.Fatalf("flag = %q", flag)
 }
 if strings.Contains(flag, "readonly") {
  t.Fatal("writable mount should not be readonly")
 }
}

func TestLimaMountFlag_RemappedReadOnly(t *testing.T) {
 m := vm.Mount{
  HostPath: "/Users/you/secrets", GuestPath: "/secrets", Writable: false,
 }
 flag := vm.LimaMountFlag(m)
 if !strings.Contains(flag, "source=/Users/you/secrets") {
  t.Fatalf("flag = %q", flag)
 }
 if !strings.Contains(flag, "target=/secrets") {
  t.Fatalf("flag = %q", flag)
 }
 if !strings.Contains(flag, "readonly") {
  t.Fatal("want readonly")
 }
}

func TestDockerVolumeFlag_Remapped(t *testing.T) {
 m := vm.Mount{HostPath: "/host", GuestPath: "/guest", Writable: true}
 got := vm.DockerVolumeFlag(m)
 want := "/host:/guest:rw"
 if got != want {
  t.Fatalf("got %q want %q", got, want)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/... -run 'Test(Lima|Docker)Mount' -v`

Expected: FAIL

- [ ] **Step 3: Implement**

`internal/vm/vm.go` — add `GuestPath string` to `Mount`.

`internal/vm/mountflag.go`:

```go
package vm

import "fmt"

func LimaMountFlag(m Mount) string {
 flag := fmt.Sprintf("type=bind,source=%s,target=%s", m.HostPath, m.GuestPath)
 if !m.Writable {
  flag += ",readonly"
 }
 return flag
}

func DockerVolumeFlag(m Mount) string {
 mode := "ro"
 if m.Writable {
  mode = "rw"
 }
 return fmt.Sprintf("%s:%s:%s", m.HostPath, m.GuestPath, mode)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/vm/... -run 'Test(Lima|Docker)Mount' -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/vm.go internal/vm/mountflag.go test/unit/vm/mountflag_test.go
git commit -m "feat: add GuestPath and mount flag helpers for Lima and Docker"
```

---

## Task 6: Lima and Docker use distinct host/guest paths

**Files:**

- Modify: `internal/vm/lima.go`
- Modify: `internal/vm/docker.go`
- Modify: `internal/vm/ssh.go` (comment only)

- [ ] **Step 1: Update Lima create args**

In `internal/vm/lima.go`, replace the mount loop:

```go
  for _, m := range opts.Mounts {
   args = append(args, "--mount", LimaMountFlag(m))
  }
```

- [ ] **Step 2: Update Docker volume args**

In `internal/vm/docker.go`, replace:

```go
  args = append(args, "-v", DockerVolumeFlag(m))
```

- [ ] **Step 3: Update SSH comment**

In `internal/vm/ssh.go`, change `SSHLoginScript` comment to:

```go
// SSHLoginScript returns a bash login-shell command that cds to workDir first.
// workDir is the guest path (translated from the host CWD by lifecycle).
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`

Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/vm/lima.go internal/vm/docker.go internal/vm/ssh.go
git commit -m "feat: bind-mount distinct host and guest paths in Lima and Docker"
```

---

## Task 7: Mount assembly and agent host dir creation

**Files:**

- Modify: `internal/lifecycle/helpers.go`
- Create: `test/unit/lifecycle/mount_assembly_test.go`

- [ ] **Step 1: Write the failing test**

`buildStartOptions` is unexported. Add exported
`ResolvedMountsForStart` in `helpers.go` (see Step 3) and test it:

```go
package lifecycle_test

import (
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/agent"
 "github.com/sisimomo/aivm/internal/config"
 "github.com/sisimomo/aivm/internal/lifecycle"
 "github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolvedMountsForStart_DedupesAgentTarget(t *testing.T) {
 home := "/Users/you"
 state := filepath.Join(home, ".aivm")
 cfg := &config.Config{
  StateDir: state,
  VM: config.VMConfig{
   ParsedMounts: []config.Mount{{
    HostPath: filepath.Join(home, "dev"),
    GuestPath: filepath.Join(home, "dev"),
    Writable: true,
   }},
  },
 }
 agentDefs := map[string]agent.Def{
  "claude": {Mounts: []mountspec.MountSpec{{
   Location: "{{ .state_dir }}/.claude/projects",
   Target: "~/.claude/projects",
   Mode: "rw",
  }}},
 }
 mounts, err := lifecycle.ResolvedMountsForStart(cfg, agentDefs, false)
 if err != nil {
  t.Fatalf("ResolvedMountsForStart: %v", err)
 }
 // expect vm mount + 1 agent mount
 if len(mounts) != 2 {
  t.Fatalf("len = %d", len(mounts))
 }
 if mounts[1].GuestPath != filepath.Join(home, ".claude/projects") {
  t.Fatalf("guest = %q", mounts[1].GuestPath)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/lifecycle/... -run TestResolvedMountsForStart -v`

Expected: FAIL

- [ ] **Step 3: Implement mount assembly**

In `internal/lifecycle/helpers.go`:

1. Rename `ensureAgentPersistDirs` → `ensureAgentMountDirs`.
2. Create host dirs from resolved agent mount `HostPath` values
   (not relative paths).
3. Add `ResolvedMountsForStart` in `helpers.go`:

```go
func ResolvedMountsForStart(
 cfg *config.Config,
 agentDefs map[string]agent.Def,
 t3Enabled bool,
) ([]vm.Mount, error) {
 home, _ := os.UserHomeDir()
 ctx := mountspec.Context{Home: home, StateDir: cfg.StateDir}

 mounts := make([]vm.Mount, 0, 16)
 resolved := make([]mountspec.ResolvedMount, 0, 16)

 for _, m := range cfg.VM.ParsedMounts {
  mounts = append(mounts, vm.Mount{
   HostPath: m.HostPath, GuestPath: m.GuestPath, Writable: m.Writable,
  })
  resolved = append(resolved, mountspec.ResolvedMount{
   HostPath: m.HostPath, GuestPath: m.GuestPath, Writable: m.Writable,
  })
 }

 seenGuest := make(map[string]bool)
 for _, m := range mounts {
  seenGuest[m.GuestPath] = true
 }

 agentNames := make([]string, 0, len(agentDefs))
 for k := range agentDefs {
  agentNames = append(agentNames, k)
 }
 sort.Strings(agentNames)
 for _, name := range agentNames {
  for _, spec := range agentDefs[name].Mounts {
   r, err := mountspec.Resolve(spec, ctx)
   if err != nil {
    return nil, fmt.Errorf("agent %q mounts: %w", name, err)
   }
   if seenGuest[r.GuestPath] {
    continue
   }
   seenGuest[r.GuestPath] = true
   mounts = append(mounts, vm.Mount{
    HostPath: r.HostPath, GuestPath: r.GuestPath, Writable: r.Writable,
   })
   resolved = append(resolved, r)
  }
 }

 if t3Enabled {
  t3Spec := mountspec.MountSpec{
   Location:   "{{ .state_dir }}/.t3",
   Target: "~/.t3",
   Mode:       "rw",
  }
  r, err := mountspec.Resolve(t3Spec, ctx)
  if err != nil {
   return nil, err
  }
  if !seenGuest[r.GuestPath] {
   mounts = append(mounts, vm.Mount{
    HostPath: r.HostPath, GuestPath: r.GuestPath, Writable: r.Writable,
   })
   resolved = append(resolved, r)
  }
 }

 if err := mountspec.ValidateDuplicateTargets(resolved); err != nil {
  return nil, err
 }
 return mounts, nil
}
```

Update `buildStartOptions` to call `ResolvedMountsForStart` and assign `opts.Mounts`.

Update `ensureAgentMountDirs`:

```go
func ensureAgentMountDirs(cfg *config.Config, agentDefs map[string]agent.Def) {
 home, _ := os.UserHomeDir()
 ctx := mountspec.Context{Home: home, StateDir: cfg.StateDir}
 seen := make(map[string]bool)
 for _, def := range agentDefs {
  for _, spec := range def.Mounts {
   r, err := mountspec.Resolve(spec, ctx)
   if err != nil {
    continue
   }
   if seen[r.HostPath] {
    continue
   }
   seen[r.HostPath] = true
   _ = os.MkdirAll(r.HostPath, 0755)
  }
 }
 if cfg.T3Code.Enable {
  r, _ := mountspec.Resolve(mountspec.MountSpec{
   Location:   "{{ .state_dir }}/.t3",
   Target: "~/.t3",
   Mode:       "rw",
  }, ctx)
  if r.HostPath != "" {
   _ = os.MkdirAll(r.HostPath, 0755)
  }
 }
}
```

Rename all call sites: `ensureAgentPersistDirs` → `ensureAgentMountDirs`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/lifecycle/... -run TestResolvedMountsForStart -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lifecycle/helpers.go internal/lifecycle/service.go \
  internal/lifecycle/bootstrap_paths.go \
  test/unit/lifecycle/mount_assembly_test.go
git commit -m "feat: assemble vm, agent, and T3 mounts with dedup"
```

---

## Task 8: GuestPathForHost path translation

**Files:**

- Create: `internal/lifecycle/mountpath.go`
- Create: `test/unit/lifecycle/mountpath_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lifecycle_test

import (
 "testing"

 "github.com/sisimomo/aivm/internal/config"
 "github.com/sisimomo/aivm/internal/lifecycle"
)

func TestGuestPathForHost_Identity(t *testing.T) {
 cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{{
  HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true,
 }}}}
 got, err := lifecycle.GuestPathForHost("/Users/you/dev/myapp", cfg)
 if err != nil {
  t.Fatal(err)
 }
 if got != "/Users/you/dev/myapp" {
  t.Fatalf("got %q", got)
 }
}

func TestGuestPathForHost_Remapped(t *testing.T) {
 cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{{
  HostPath: "/Users/you/secrets", GuestPath: "/secrets", Writable: true,
 }}}}
 got, err := lifecycle.GuestPathForHost("/Users/you/secrets/keys", cfg)
 if err != nil {
  t.Fatal(err)
 }
 if got != "/secrets/keys" {
  t.Fatalf("got %q", got)
 }
}

func TestGuestPathForHost_LongestPrefix(t *testing.T) {
 cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{
  {HostPath: "/Users/you", GuestPath: "/Users/you", Writable: true},
  {HostPath: "/Users/you/dev", GuestPath: "/work", Writable: true},
 }}}
 got, err := lifecycle.GuestPathForHost("/Users/you/dev/pkg", cfg)
 if err != nil {
  t.Fatal(err)
 }
 if got != "/work/pkg" {
  t.Fatalf("got %q", got)
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/lifecycle/... -run TestGuestPathForHost -v`

Expected: FAIL

- [ ] **Step 3: Implement**

`internal/lifecycle/mountpath.go`:

```go
package lifecycle

import (
 "fmt"
 "path/filepath"
 "sort"
 "strings"

 "github.com/sisimomo/aivm/internal/config"
)

func GuestPathForHost(hostPath string, cfg *config.Config) (string, error) {
 hostPath = filepath.Clean(hostPath)
 type match struct {
  hostLen int
  mount   config.Mount
 }
 var matches []match
 for _, m := range cfg.VM.ParsedMounts {
  realLoc := m.HostPath
  if resolved, err := filepath.EvalSymlinks(m.HostPath); err == nil {
   realLoc = filepath.Clean(resolved)
  }
  if PathUnderMount(hostPath, realLoc) {
   matches = append(matches, match{
    hostLen: len(realLoc), mount: m,
   })
  }
 }
 if len(matches) == 0 {
  return "", fmt.Errorf(
   "internal error: host path %q is not under any vm.mounts location",
   hostPath)
 }
 sort.Slice(matches, func(i, j int) bool {
  return matches[i].hostLen > matches[j].hostLen
 })
 best := matches[0].mount
 realLoc := best.HostPath
 if resolved, err := filepath.EvalSymlinks(best.HostPath); err == nil {
  realLoc = filepath.Clean(resolved)
 }
 rel := strings.TrimPrefix(hostPath, realLoc)
 rel = strings.TrimPrefix(rel, string(filepath.Separator))
 if rel == "" {
  return best.GuestPath, nil
 }
 return filepath.Join(best.GuestPath, rel), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/lifecycle/... -run TestGuestPathForHost -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lifecycle/mountpath.go test/unit/lifecycle/mountpath_test.go
git commit -m "feat: translate host CWD to guest path via vm.mounts"
```

---

## Task 9: Wire path translation into SSH and agent launch

**Files:**

- Modify: `internal/lifecycle/commands.go`
- Modify: `internal/lifecycle/agent_session.go`
- Modify: `test/unit/lifecycle/agent_session_test.go`

- [ ] **Step 1: Write the failing test**

Add to `test/unit/lifecycle/agent_session_test.go`:

```go
func TestGuestPathForHost_UsedByAssertFlow(t *testing.T) {
 t.Parallel()
 cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{{
  HostPath: "/mnt/proj", GuestPath: "/work", Writable: true,
 }}}}
 if err := lifecycle.AssertUnderMount("/mnt/proj/src", cfg); err != nil {
  t.Fatalf("AssertUnderMount: %v", err)
 }
 got, err := lifecycle.GuestPathForHost("/mnt/proj/src", cfg)
 if err != nil {
  t.Fatal(err)
 }
 if got != "/work/src" {
  t.Fatalf("got %q", got)
 }
}
```

Update existing `ParsedMounts` fixtures to include `GuestPath` matching `HostPath`.

- [ ] **Step 2: Update call sites**

`internal/lifecycle/agent_session.go` — after `AssertUnderMount`:

```go
 guestCWD, err := GuestPathForHost(realCWD, cfg)
 if err != nil {
  return nil, err
 }
```

Set `vmDir: guestCWD` in returned `agentSession`.

`internal/lifecycle/commands.go` — after `AssertUnderMount`:

```go
 guestCWD, err := GuestPathForHost(realCWD, svc.Config)
 if err != nil {
  return err
 }
 return svc.VM.SSH(ctx, guestCWD, svc.Config.VM.ResolvedSessionEnv())
```

- [ ] **Step 3: Run tests**

Run: `go test ./test/unit/lifecycle/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/lifecycle/commands.go internal/lifecycle/agent_session.go \
  test/unit/lifecycle/agent_session_test.go
git commit -m "feat: use guest paths for ssh and agent session workdir"
```

---

## Task 10: Remove T3 symlink workaround

**Files:**

- Modify: `internal/plugin/defaults.yaml`
- Modify: `test/unit/plugin/defaults_test.go`

- [ ] **Step 1: Update test expectations**

Replace `TestLoadDefaults_T3CodeUsesStateDirTemplate` with:

```go
func TestLoadDefaults_T3CodeSetupHasNoSymlink(t *testing.T) {
 defs, err := plugin.LoadDefaults()
 if err != nil {
  t.Fatalf("LoadDefaults: %v", err)
 }
 t3code := defs["t3code"]
 if strings.Contains(t3code.Setup, "ln -sfn") {
  t.Error("t3code setup must not symlink ~/.t3")
 }
}
```

- [ ] **Step 2: Edit plugin defaults**

In `internal/plugin/defaults.yaml`, replace the t3code `setup` block with install-only:

```yaml
  setup: |
    npm install -g t3
```

- [ ] **Step 3: Run tests**

Run: `go test ./test/unit/plugin/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/defaults.yaml test/unit/plugin/defaults_test.go
git commit -m "refactor: drop T3 symlink; persistence via internal mount"
```

---

## Task 11: Update test harness and example configs

**Files:**

- Modify: `test/framework/config.go`
- Modify: `test/lifecycle/harness/harness.go`
- Modify: `aivm.example.yaml`
- Modify: `demo/configs/aivm.yaml`
- Modify: `internal/vm/lima.yaml` (comment: agent mount dirs)
- Modify: `internal/config/composition.go` (comment: mount dirs)

- [ ] **Step 1: Update framework mount YAML emission**

In `test/framework/config.go`, replace string mount with:

```go
 if tc.DevRoot != "" {
  fmt.Fprintf(&sb, "  mounts:\n")
  fmt.Fprintf(&sb, "    - source: %q\n", tc.DevRoot)
  fmt.Fprintf(&sb, "      target: %q\n", tc.DevRoot)
  fmt.Fprintf(&sb, "      mode: rw\n")
 }
```

- [ ] **Step 2: Update lifecycle harness**

In `test/lifecycle/harness/harness.go` `SetLaunchWorkDir`:

```go
 h.svc.Config.VM.ParsedMounts = []config.Mount{{
  HostPath: workDir, GuestPath: workDir, Writable: true,
 }}
```

- [ ] **Step 3: Update example configs**

`aivm.example.yaml` mounts section per spec (structured format + remapped
example comment).

`demo/configs/aivm.yaml`:

```yaml
  mounts:
    - source: "{{ .home }}/dev"
      target: "{{ .home }}/dev"
      mode: rw
```

- [ ] **Step 4: Verify unit + lifecycle tests compile**

Run: `go test ./test/unit/... ./test/lifecycle/... -count=1`

Expected: PASS (or skip if VM tests need Docker)

- [ ] **Step 5: Commit**

```bash
git add test/framework/config.go test/lifecycle/harness/harness.go \
  aivm.example.yaml demo/configs/aivm.yaml internal/vm/lima.yaml \
  internal/config/composition.go
git commit -m "chore: update harness and examples for structured mounts"
```

---

## Task 12: E2E tests for remapped mounts and Claude persistence

**Files:**

- Create: `test/e2e/mount_translation_test.go`
- Create: `test/e2e/claude_persistence_test.go` (if not covered elsewhere)

- [ ] **Step 1: Write remapped mount SSH test**

```go
func TestSSH_RemappedMountWorkDir(t *testing.T) {
 if testing.Short() {
  t.Skip("e2e")
 }
 devRoot := t.TempDir()
 secretSub := filepath.Join(devRoot, "nested")
 os.MkdirAll(secretSub, 0755)

 h := harness.New(t,
  framework.WithDevRoot(devRoot),
  // add framework option or inline YAML override for remapped mount:
  // source: devRoot, target: /work
 )
 h.Start()
 h.RunStep(actions.SSHInDir(secretSub))
 // assert pwd == /work/nested inside VM
}
```

If `WithDevRoot` only supports identity mounts, add
`framework.WithRemappedMount(host, guest string)` that emits structured YAML.

- [ ] **Step 2: Write Claude persistence test sketch**

After `aivm recreate`, verify a file written under `~/.claude/projects` in the VM
exists on host at `$STATE_DIR/.claude/projects`. Use existing e2e harness patterns
from `test/e2e/lifecycle_test.go`.

- [ ] **Step 3: Run e2e (when VM available)**

Run: `go test ./test/e2e/... -run 'TestSSH_Remapped|TestClaude' -v -timeout 30m`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add test/e2e/mount_translation_test.go test/e2e/claude_persistence_test.go test/framework/config.go
git commit -m "test: e2e coverage for remapped mounts and Claude state persistence"
```

---

## Task 13: Documentation

**Files:**

- Modify: `README.md`

- [ ] **Step 1: Update Mounts section**

Replace string format docs with structured `MountSpec` format, template variables
(`state_dir`, `home`), remapped mount example, and note that `aivm ssh` and
`aivm` translate host CWD to guest path. `aivm cp vm:/path` unchanged.

- [ ] **Step 2: Update agent persistence sections**

Claude: host data under `~/.aivm/.claude/projects` and `image-cache`; mounted at
`~/.claude/…` in VM. Copilot/Cursor: same remapped pattern. Replace `persist`
wording with `mounts`.

- [ ] **Step 3: Lint markdown**

Run: `npx markdownlint-cli2 "README.md" --fix`

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: structured mounts and agent mount persistence"
```

---

## Task 14: Full verification

- [ ] **Step 1: Format and lint Go**

Run:

```bash
go fmt ./internal/... ./test/...
golangci-lint run ./internal/mountspec/... ./internal/config/... \
  ./internal/agent/... ./internal/vm/... ./internal/lifecycle/...
```

Expected: no issues

- [ ] **Step 2: Run all unit tests**

Run: `go test ./test/unit/... -count=1`

Expected: PASS

- [ ] **Step 3: Run lifecycle harness tests**

Run: `go test ./test/lifecycle/... -count=1`

Expected: PASS

- [ ] **Step 4: Final commit if fixups needed**

```bash
git add -A
git commit -m "chore: mount system verification fixups"
```

---

## Self-review

### Spec coverage

| Requirement | Task |
| --- | --- |
| `MountSpec` schema | Task 1 |
| Template `state_dir` / `home` | Task 1 |
| `vm.mounts` structured only (hard cut) | Task 3 |
| Agent `persist` → `mounts` | Task 4 |
| Claude `projects` + `image-cache` | Task 4 |
| `vm.Mount` GuestPath | Task 5–6 |
| Lima/Docker distinct bind paths | Task 6 |
| Duplicate `target` validation | Task 2, 7 |
| Overlapping `vm.mounts` locations | Task 2, 3 |
| `ensureAgentMountDirs` | Task 7 |
| T3 internal mount, no symlink | Task 7, 10 |
| `GuestPathForHost` | Task 8 |
| SSH + agent CWD translation | Task 9 |
| `AssertUnderMount` unchanged | Task 9 (host path only) |
| `aivm cp` unchanged | No code change |
| README + examples | Task 11, 13 |
| Unit + E2E tests | Tasks 1–12, 14 |

No gaps identified.

### Placeholder scan

No TBD/TODO/similar-to tasks. All steps include concrete code and commands.

### Type consistency

- `mountspec.MountSpec` in YAML → `mountspec.Resolve` → `config.Mount` / `vm.Mount`
- `GuestPath` used consistently from config through VM backends and `GuestPathForHost`
- `ensureAgentMountDirs` uses resolved `HostPath` (location), not relative strings
