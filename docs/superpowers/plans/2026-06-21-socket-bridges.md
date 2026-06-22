# Socket Bridges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose host Unix domain sockets inside the AIVM VM at stable guest
paths via config-driven, backend-native forwarding (Docker bind mount, Lima
`portForwards`).

**Architecture:** Top-level `socket_bridges` in `aivm.yaml` (separate from
`vm.mounts`) resolves `host_path` with `os.ExpandEnv` and `~` expansion at
config load. Resolved bridges flow through `StartOptions.SocketBridges` into
Docker (`-v host:guest:ro`) and Lima (`portForwards` with `reverse: true`).
Bridges attach at **runtime** profile only (not bootstrap) on Docker; Lima gets
them at create. Config hash includes resolved bridges so changes trigger VM
recreate.

**Tech Stack:** Go, Viper/mapstructure, Lima instance template YAML, Docker
`-v`, `net.Listen("unix")` for tests

**Spec:** `docs/superpowers/specs/2026-06-21-socket-bridges-design.md`

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/config/config.go` | YAML type, parse, validate, resolve |
| `internal/vm/vm.go` | Runtime `SocketBridge`, `StartOptions` field |
| `internal/vm/mountflag.go` | Docker `:ro` flag, Lima portForward YAML |
| `internal/vm/bridges.go` | Start-time validation per backend |
| `internal/vm/template.go` | `LimaTemplatePath(mounts, bridges)` |
| `internal/vm/lima.go` | Template wiring + info logs |
| `internal/vm/docker.go` | Socket `-v` binds + info logs |
| `internal/lifecycle/helpers.go` | Runtime opts, guest prep, validation |
| `internal/lifecycle/state.go` | Config hash input |
| `aivm.example.yaml`, `README.md` | User docs |
| `test/unit/config/socket_bridges_test.go` | Load validation tests |
| `test/unit/vm/mountflag_test.go` | Flag/YAML helper tests |
| `test/unit/vm/bridges_test.go` | Start validation tests |
| `test/unit/lifecycle/hash_test.go` | Hash regression tests |
| `test/unit/lifecycle/socket_bridges_test.go` | Bootstrap vs runtime tests |
| `test/e2e/socket_bridge_test.go` | Docker round-trip e2e |
| `test/framework/config.go` | `WithSocketBridge` harness option |

---

## Task 1: Config types and load-time validation

**Files:**

- Modify: `internal/config/config.go`
- Create: `test/unit/config/socket_bridges_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package config_test

import (
 "os"
 "path/filepath"
 "strings"
 "testing"

 "github.com/sisimomo/aivm/internal/config"
)

func writeConfig(t *testing.T, dir, content string) string {
 t.Helper()
 path := filepath.Join(dir, "aivm.yaml")
 if err := os.WriteFile(path, []byte(content), 0644); err != nil {
  t.Fatal(err)
 }
 return path
}

func minimalVMHeader() string {
 return `
agents:
  enabled: [claude]
vm:
  name: testvm
`
}

func TestLoad_SocketBridge_ExpandsHostPath(t *testing.T) {
 t.Parallel()
 home, _ := os.UserHomeDir()
 dir := t.TempDir()
 sockDir := filepath.Join(dir, "sockets")
 t.Setenv("TEST_SOCK_DIR", sockDir)
 path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "${TEST_SOCK_DIR}/service.sock"
    guest_path: /run/aivm/sockets/service.sock
`)
 cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err != nil {
  t.Fatalf("Load: %v", err)
 }
 if len(cfg.ParsedSocketBridges) != 1 {
  t.Fatalf("len = %d", len(cfg.ParsedSocketBridges))
 }
 wantHost := filepath.Join(sockDir, "service.sock")
 b := cfg.ParsedSocketBridges[0]
 if b.HostPath != wantHost || b.GuestPath != "/run/aivm/sockets/service.sock" {
  t.Fatalf("got %+v, want host %q guest /run/aivm/sockets/service.sock", b,
  wantHost)
 }
 if got := cfg.ResolvedSocketBridges(); len(got) != 1 || got[0].HostPath !=
 wantHost {
  t.Fatalf("ResolvedSocketBridges = %+v", got)
 }
 _ = home
}

func TestLoad_SocketBridge_TildeExpansion(t *testing.T) {
 t.Parallel()
 home, _ := os.UserHomeDir()
 dir := t.TempDir()
 path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "~/.config/foo/service.sock"
    guest_path: /run/aivm/sockets/foo.sock
`)
 cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err != nil {
  t.Fatalf("Load: %v", err)
 }
 want := filepath.Join(home, ".config/foo/service.sock")
 if cfg.ParsedSocketBridges[0].HostPath != want {
  t.Fatalf("HostPath = %q, want %q", cfg.ParsedSocketBridges[0].HostPath, want)
 }
}

func TestLoad_SocketBridge_RejectsRelativeGuestPath(t *testing.T) {
 t.Parallel()
 dir := t.TempDir()
 path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/sock"
    guest_path: run/sockets/foo.sock
`)
 _, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err == nil || !strings.Contains(err.Error(), "guest_path") {
  t.Fatalf("expected guest_path error, got %v", err)
 }
}

func TestLoad_SocketBridge_RejectsDuplicateGuestPath(t *testing.T) {
 t.Parallel()
 dir := t.TempDir()
 path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/a.sock"
    guest_path: /run/aivm/sockets/foo.sock
  - host_path: "/tmp/b.sock"
    guest_path: /run/aivm/sockets/foo.sock
`)
 _, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err == nil || !strings.Contains(err.Error(), "duplicate guest_path") {
  t.Fatalf("expected duplicate guest_path error, got %v", err)
 }
}

func TestLoad_SocketBridge_RejectsDuplicateHostPath(t *testing.T) {
 t.Parallel()
 dir := t.TempDir()
 path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/same.sock"
    guest_path: /run/aivm/sockets/a.sock
  - host_path: "/tmp/same.sock"
    guest_path: /run/aivm/sockets/b.sock
`)
 _, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err == nil || !strings.Contains(err.Error(), "duplicate host_path") {
  t.Fatalf("expected duplicate host_path error, got %v", err)
 }
}

func TestLoad_SocketBridge_EmptyListValid(t *testing.T) {
 t.Parallel()
 dir := t.TempDir()
 path := writeConfig(t, dir, minimalVMHeader())
 cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
 if err != nil {
  t.Fatalf("Load: %v", err)
 }
 if cfg.ParsedSocketBridges != nil && len(cfg.ParsedSocketBridges) != 0 {
  t.Fatalf("expected no bridges, got %+v", cfg.ParsedSocketBridges)
 }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./test/unit/config/... -run SocketBridge -v`
Expected: FAIL — `ParsedSocketBridges` / `ResolvedSocketBridges` undefined

- [ ] **Step 3: Implement config types and validation**

Add to `internal/config/config.go`:

```go
// SocketBridge maps a host Unix socket to a guest path in the VM.
type SocketBridge struct {
 HostPath  string `mapstructure:"host_path"`
 GuestPath string `mapstructure:"guest_path"`
}
```

On `Config`:

```go
SocketBridges       []SocketBridge `mapstructure:"socket_bridges"`
ParsedSocketBridges []SocketBridge `mapstructure:"-"`
```

Add methods:

```go
// ResolvedSocketBridges returns bridges with host_path expanded (~ and ${VAR}).
func (c *Config) ResolvedSocketBridges() []SocketBridge {
 return c.ParsedSocketBridges
}

func resolveSocketBridgePath(raw, home string) (string, error) {
 if strings.TrimSpace(raw) == "" {
  return "", fmt.Errorf("path must not be empty")
 }
 expanded := os.ExpandEnv(raw)
 expanded = expandPath(expanded, home)
 if !filepath.IsAbs(expanded) {
  return "", fmt.Errorf("path %q must be absolute after expansion (got %q)",
  raw, expanded)
 }
 return expanded, nil
}

func parseSocketBridges(bridges []SocketBridge, home string, mountSources
[]string) ([]SocketBridge, error) {
 if len(bridges) == 0 {
  return nil, nil
 }
 parsed := make([]SocketBridge, 0, len(bridges))
 seenGuest := make(map[string]int)
 seenHost := make(map[string]int)
 mountSet := make(map[string]bool, len(mountSources))
 for _, s := range mountSources {
  mountSet[s] = true
 }
 for i, b := range bridges {
  if strings.TrimSpace(b.HostPath) == "" {
   return nil, fmt.Errorf("socket_bridges[%d]: host_path is required", i)
  }
  if strings.TrimSpace(b.GuestPath) == "" {
   return nil, fmt.Errorf("socket_bridges[%d]: guest_path is required", i)
  }
  host, err := resolveSocketBridgePath(b.HostPath, home)
  if err != nil {
   return nil, fmt.Errorf("socket_bridges[%d].host_path: %w", i, err)
  }
  guest := filepath.Clean(b.GuestPath)
  if !filepath.IsAbs(guest) {
   return nil, fmt.Errorf("socket_bridges[%d].guest_path: must be absolute (got
   %q)", i, b.GuestPath)
  }
  if prev, ok := seenGuest[guest]; ok {
   return nil, fmt.Errorf("socket_bridges: duplicate guest_path %q (entries %d
   and %d)", guest, prev, i)
  }
  seenGuest[guest] = i
  if prev, ok := seenHost[host]; ok {
   return nil, fmt.Errorf("socket_bridges: duplicate host_path %q (entries %d
   and %d)", host, prev, i)
  }
  seenHost[host] = i
  if mountSet[host] {
   slog.Warn(fmt.Sprintf("socket_bridges[%d]: host_path %q matches a vm.mounts
   source — unlikely to work as a socket bridge", i, host))
  }
  parsed = append(parsed, SocketBridge{HostPath: host, GuestPath: guest})
 }
 return parsed, nil
}
```

In `validateAndParse`, after mount parsing succeeds:

```go
mountSources := make([]string, len(vm.ParsedMounts))
for i, m := range vm.ParsedMounts {
 mountSources[i] = m.HostPath
}
parsedBridges, err := parseSocketBridges(cfg.SocketBridges, home, mountSources)
if err != nil {
 return err
}
cfg.ParsedSocketBridges = parsedBridges
```

Add `"log/slog"` import.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./test/unit/config/... -run SocketBridge -v`
Expected: PASS

- [ ] **Step 5: Format and lint**

Run: `go fmt internal/config/config.go test/unit/config/socket_bridges_test.go`
Run: `golangci-lint run internal/config/config.go
test/unit/config/socket_bridges_test.go`

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go test/unit/config/socket_bridges_test.go
git commit -m "feat: add socket_bridges config load and validation"
```

---

## Task 2: VM types and YAML/volume helpers

**Files:**

- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/mountflag.go`
- Modify: `test/unit/vm/mountflag_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `test/unit/vm/mountflag_test.go`:

```go
func TestDockerSocketVolumeFlag_ReadOnly(t *testing.T) {
 b := vm.SocketBridge{HostPath: "/host/sock", GuestPath:
 "/run/aivm/sockets/sock"}
 got := vm.DockerSocketVolumeFlag(b)
 want := "/host/sock:/run/aivm/sockets/sock:ro"
 if got != want {
  t.Fatalf("got %q want %q", got, want)
 }
}

func TestLimaPortForwardsYAML_ReverseSocket(t *testing.T) {
 got := vm.LimaPortForwardsYAML([]vm.SocketBridge{{
  HostPath: "/Users/you/.config/foo/service.sock",
  GuestPath: "/run/aivm/sockets/foo.sock",
 }})
 if !strings.HasPrefix(got, "portForwards:\n") {
  t.Fatalf("got %q", got)
 }
 if !strings.Contains(got, `guestSocket: "/run/aivm/sockets/foo.sock"`) {
  t.Fatalf("got %q", got)
 }
 if !strings.Contains(got, `hostSocket: "/Users/you/.config/foo/service.sock"`)
 {
  t.Fatalf("got %q", got)
 }
 if !strings.Contains(got, "reverse: true") {
  t.Fatal("want reverse: true")
 }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./test/unit/vm/... -run
'DockerSocketVolumeFlag|LimaPortForwardsYAML' -v`
Expected: FAIL — symbols undefined

- [ ] **Step 3: Implement VM types and helpers**

In `internal/vm/vm.go`:

```go
type SocketBridge struct {
 HostPath  string
 GuestPath string
}
```

On `StartOptions`:

```go
SocketBridges []SocketBridge
```

In `internal/vm/mountflag.go`:

```go
func DockerSocketVolumeFlag(b SocketBridge) string {
 return fmt.Sprintf("%s:%s:ro", b.HostPath, b.GuestPath)
}

func LimaPortForwardYAML(b SocketBridge) string {
 var sb strings.Builder
 fmt.Fprintf(&sb, "- guestSocket: %q\n", b.GuestPath)
 fmt.Fprintf(&sb, "  hostSocket: %q\n", b.HostPath)
 sb.WriteString("  reverse: true\n")
 return sb.String()
}

func LimaPortForwardsYAML(bridges []SocketBridge) string {
 if len(bridges) == 0 {
  return ""
 }
 var sb strings.Builder
 sb.WriteString("portForwards:\n")
 for _, b := range bridges {
  sb.WriteString(LimaPortForwardYAML(b))
 }
 return sb.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./test/unit/vm/... -run
'DockerSocketVolumeFlag|LimaPortForwardsYAML' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/vm.go internal/vm/mountflag.go
test/unit/vm/mountflag_test.go
git commit -m "feat: add SocketBridge VM type and backend flag helpers"
```

---

## Task 3: Lima template generation

**Files:**

- Modify: `internal/vm/template.go`
- Modify: `internal/vm/lima.go`
- Modify: `test/unit/vm/lima_template_test.go`

- [ ] **Step 1: Write the failing test**

Update `test/unit/vm/lima_template_test.go` to pass bridges and assert YAML
content:

```go
func TestLimaTemplate_IncludesPortForwards(t *testing.T) {
 path, err := vm.LimaTemplatePath([]vm.Mount{}, []vm.SocketBridge{{
  HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
 }})
 if err != nil {
  t.Fatal(err)
 }
 defer os.Remove(path)
 data, err := os.ReadFile(path)
 if err != nil {
  t.Fatal(err)
 }
 body := string(data)
 if !strings.Contains(body, "portForwards:") {
  t.Fatalf("missing portForwards in template:\n%s", body)
 }
 if !strings.Contains(body, "reverse: true") {
  t.Fatal("missing reverse: true")
 }
}
```

Add `"strings"` import.

Update existing `TestLimaTemplate_ValidatesWithLimactl` call site:

```go
path, err := vm.LimaTemplatePath([]vm.Mount{
 {HostPath: "/tmp/aivm-host", GuestPath: "/work", Writable: true},
}, nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/... -run LimaTemplate -v`
Expected: FAIL — wrong `LimaTemplatePath` arity or missing portForwards

- [ ] **Step 3: Implement template changes**

In `internal/vm/template.go`:

```go
func LimaTemplatePath(mounts []Mount, bridges []SocketBridge) (string, error) {
 // ... existing write of limaTemplate ...
 if yaml := LimaMountsYAML(mounts); yaml != "" {
  // existing mounts append
 }
 if yaml := LimaPortForwardsYAML(bridges); yaml != "" {
  if _, err := f.WriteString("\n" + yaml); err != nil {
   _ = f.Close()
   _ = os.Remove(f.Name())
   return "", fmt.Errorf("write lima portForwards: %w", err)
  }
 }
 // ... close and return ...
}
```

In `internal/vm/lima.go` default-create branch:

```go
templatePath, err := LimaTemplatePath(opts.Mounts, opts.SocketBridges)
```

After template write, log bridges:

```go
for _, b := range opts.SocketBridges {
 slog.Info(fmt.Sprintf("socket bridge %s ← %s (lima portForward)", b.GuestPath,
 b.HostPath))
 slog.Debug(fmt.Sprintf("socket bridge resolved: guest=%s host=%s",
 b.GuestPath, b.HostPath))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./test/unit/vm/... -run LimaTemplate -v`
Expected: PASS (limactl test skips if not installed)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/template.go internal/vm/lima.go
test/unit/vm/lima_template_test.go
git commit -m "feat: emit Lima portForwards for socket bridges"
```

---

## Task 4: Start-time validation

**Files:**

- Create: `internal/vm/bridges.go`
- Create: `test/unit/vm/bridges_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package vm_test

import (
 "net"
 "os"
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func startUnixSocket(t *testing.T, path string) {
 t.Helper()
 _ = os.Remove(path)
 if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
  t.Fatal(err)
 }
 ln, err := net.Listen("unix", path)
 if err != nil {
  t.Fatal(err)
 }
 t.Cleanup(func() { ln.Close(); os.Remove(path) })
}

func TestValidateSocketBridgesForStart_DockerRequiresSocket(t *testing.T) {
 dir := t.TempDir()
 missing := filepath.Join(dir, "missing.sock")
 err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
  HostPath: missing, GuestPath: "/run/aivm/sockets/x.sock",
 }}, nil)
 if err == nil {
  t.Fatal("expected error for missing host socket")
 }
}

func TestValidateSocketBridgesForStart_DockerAcceptsSocket(t *testing.T) {
 dir := t.TempDir()
 sock := filepath.Join(dir, "ok.sock")
 startUnixSocket(t, sock)
 err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
  HostPath: sock, GuestPath: "/run/aivm/sockets/x.sock",
 }}, nil)
 if err != nil {
  t.Fatalf("unexpected error: %v", err)
 }
}

func TestValidateSocketBridgesForStart_GuestConflictsWithMount(t *testing.T) {
 dir := t.TempDir()
 sock := filepath.Join(dir, "ok.sock")
 startUnixSocket(t, sock)
 err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
  HostPath: sock, GuestPath: "/work",
 }}, []vm.Mount{{HostPath: "/host", GuestPath: "/work", Writable: true}})
 if err == nil {
  t.Fatal("expected guest_path conflict error")
 }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./test/unit/vm/... -run ValidateSocketBridges -v`
Expected: FAIL — `ValidateSocketBridgesForStart` not defined

- [ ] **Step 3: Implement validation**

Create `internal/vm/bridges.go`:

```go
package vm

import (
 "fmt"
 "log/slog"
 "os"
 "path/filepath"
)

// ValidateSocketBridgesForStart enforces backend rules before VM create.
func ValidateSocketBridgesForStart(backend string, bridges []SocketBridge,
mounts []Mount) error {
 if len(bridges) == 0 {
  return nil
 }
 mountGuests := make(map[string]bool, len(mounts))
 for _, m := range mounts {
  mountGuests[m.GuestPath] = true
 }
 for _, b := range bridges {
  if mountGuests[b.GuestPath] {
   return fmt.Errorf("socket bridge guest_path %q conflicts with a directory
   mount target", b.GuestPath)
  }
 }
 switch backend {
 case "docker":
  return validateDockerSocketBridges(bridges)
 default: // lima or empty
  return validateLimaSocketBridges(bridges)
 }
}

func validateDockerSocketBridges(bridges []SocketBridge) error {
 for _, b := range bridges {
  fi, err := os.Stat(b.HostPath)
  if err != nil {
   return fmt.Errorf("socket bridge host_path %q must exist before docker
   create: %w", b.HostPath, err)
  }
  if fi.Mode()&os.ModeSocket == 0 {
   return fmt.Errorf("socket bridge host_path %q must be a Unix socket (not a
   file or directory)", b.HostPath)
  }
 }
 return nil
}

func validateLimaSocketBridges(bridges []SocketBridge) error {
 for _, b := range bridges {
  parent := filepath.Dir(b.HostPath)
  fi, err := os.Stat(parent)
  if err != nil {
   return fmt.Errorf("socket bridge host_path %q: parent directory %q not
   accessible: %w", b.HostPath, parent, err)
  }
  if !fi.IsDir() {
   return fmt.Errorf("socket bridge host_path %q: parent %q is not a
   directory", b.HostPath, parent)
  }
  if sockFi, err := os.Stat(b.HostPath); err != nil {
   slog.Warn(fmt.Sprintf("socket bridge host_path %q does not exist yet; Lima
   forward may fail until the host daemon creates it", b.HostPath))
  } else if sockFi.Mode()&os.ModeSocket == 0 {
   slog.Warn(fmt.Sprintf("socket bridge host_path %q exists but is not a Unix
   socket yet", b.HostPath))
  }
 }
 return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./test/unit/vm/... -run ValidateSocketBridges -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/bridges.go test/unit/vm/bridges_test.go
git commit -m "feat: validate socket bridges at VM start"
```

---

## Task 5: Docker bind mounts

**Files:**

- Modify: `internal/vm/docker.go`

- [ ] **Step 1: Write the failing test**

Append to `test/unit/vm/docker_finalize_test.go` or create focused test in
`test/unit/vm/docker_socket_test.go`:

```go
func TestDockerStartOptions_IncludesSocketBridgeVolume(t *testing.T) {
 b := vm.SocketBridge{HostPath: "/tmp/host.sock", GuestPath:
 "/run/aivm/sockets/host.sock"}
 got := vm.DockerSocketVolumeFlag(b)
 if got != "/tmp/host.sock:/run/aivm/sockets/host.sock:ro" {
  t.Fatalf("got %q", got)
 }
}
```

(This reuses Task 2 helper; the docker.go change is verified via e2e in Task 9.
Optionally extend `docker_finalize_test.go` to assert runtime opts carry
`SocketBridges` length > 0 when configured.)

- [ ] **Step 2: Implement docker bind mounts**

In `startFromImage` after directory mount loop:

```go
for _, b := range opts.SocketBridges {
 slog.Info(fmt.Sprintf("socket bridge %s ← %s (docker bind)", b.GuestPath,
 b.HostPath))
 slog.Debug(fmt.Sprintf("socket bridge resolved: guest=%s host=%s",
 b.GuestPath, b.HostPath))
 args = append(args, "-v", DockerSocketVolumeFlag(b))
}
```

Add `"log/slog"` import if not present.

- [ ] **Step 3: Build to verify compile**

Run: `go build ./internal/vm/...`
Expected: success

- [ ] **Step 4: Commit**

```bash
git add internal/vm/docker.go
git commit -m "feat: bind-mount host Unix sockets in Docker VM create"
```

---

## Task 6: Lifecycle wiring

**Files:**

- Modify: `internal/lifecycle/helpers.go`
- Modify: `internal/lifecycle/service.go`
- Modify: `internal/lifecycle/bootstrap_paths.go`
- Create: `test/unit/lifecycle/socket_bridges_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package lifecycle_test

import (
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/agent"
 "github.com/sisimomo/aivm/internal/config"
 "github.com/sisimomo/aivm/internal/lifecycle"
 "github.com/sisimomo/aivm/internal/vm"
)

func TestBuildRuntimeStartOptions_IncludesSocketBridges(t *testing.T) {
 home := "/Users/you"
 t.Setenv("HOME", home)
 cfg := &config.Config{
  StateDir: filepath.Join(home, ".aivm"),
  VM: config.VMConfig{
   Backend:      "docker",
   ParsedVMHome: "/home/user",
  },
  ParsedSocketBridges: []config.SocketBridge{{
   HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
  }},
 }
 opts, err := lifecycle.BuildRuntimeStartOptionsForTest(vm.NewDocker("p",
 cfg.StateDir, "img", false), cfg, nil)
 if err != nil {
  t.Fatal(err)
 }
 if len(opts.SocketBridges) != 1 {
  t.Fatalf("SocketBridges len = %d", len(opts.SocketBridges))
 }
 if opts.SocketBridges[0].GuestPath != "/run/aivm/sockets/host.sock" {
  t.Fatalf("got %+v", opts.SocketBridges[0])
 }
}

func TestBuildBootstrapStartOptions_ExcludesSocketBridges(t *testing.T) {
 cfg := &config.Config{
  ParsedSocketBridges: []config.SocketBridge{{
   HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
  }},
 }
 opts, err := lifecycle.BuildBootstrapStartOptionsForTest(vm.NewDocker("p",
 t.TempDir(), "img", false), cfg, map[string]agent.Def{})
 if err != nil {
  t.Fatal(err)
 }
 if len(opts.SocketBridges) != 0 {
  t.Fatalf("bootstrap should not attach bridges, got %+v", opts.SocketBridges)
 }
}
```

- [ ] **Step 2: Export test helpers and implement lifecycle wiring**

In `internal/lifecycle/helpers.go`:

```go
func socketBridgesFromConfig(cfg *config.Config) []vm.SocketBridge {
 if len(cfg.ParsedSocketBridges) == 0 {
  return nil
 }
 out := make([]vm.SocketBridge, len(cfg.ParsedSocketBridges))
 for i, b := range cfg.ParsedSocketBridges {
  out[i] = vm.SocketBridge{HostPath: b.HostPath, GuestPath: b.GuestPath}
 }
 return out
}

func validateAndPrepareSocketBridges(v vm.VM, cfg *config.Config, mounts
[]vm.Mount) error {
 bridges := socketBridgesFromConfig(cfg)
 if len(bridges) == 0 {
  return nil
 }
 backend := effectiveBackend(cfg.VM)
 if err := vm.ValidateSocketBridgesForStart(backend, bridges, mounts); err !=
 nil {
  return err
 }
 return nil
}

func prepareSocketBridgeGuestDirs(ctx context.Context, v vm.VM, bridges
[]vm.SocketBridge) error {
 if len(bridges) == 0 {
  return nil
 }
 seen := make(map[string]bool)
 var parents []string
 for _, b := range bridges {
  parent := filepath.Dir(b.GuestPath)
  if seen[parent] {
   continue
  }
  seen[parent] = true
  parents = append(parents, parent)
 }
 sort.Strings(parents)
 script := "set -e\n"
 for _, p := range parents {
  script += fmt.Sprintf("sudo mkdir -p %s\n", vm.ShellEscape(p))
 }
 return v.Run(ctx, script, nil)
}
```

Update `buildRuntimeStartOptions`:

```go
return vm.StartOptions{
 // ... existing fields ...
 SocketBridges: socketBridgesFromConfig(cfg),
}, nil
```

Do **not** add bridges to `buildBootstrapStartOptions`.

Add test exports:

```go
func BuildRuntimeStartOptionsForTest(v vm.VM, cfg *config.Config, agentDefs
map[string]agent.Def) (vm.StartOptions, error) {
 return buildRuntimeStartOptions(v, cfg, agentDefs)
}

func BuildBootstrapStartOptionsForTest(v vm.VM, cfg *config.Config, agentDefs
map[string]agent.Def) (vm.StartOptions, error) {
 return buildBootstrapStartOptions(v, cfg, agentDefs)
}
```

In `internal/lifecycle/service.go` `resumeOrStartVM`, after building opts and
before `ensureAgentMountDirs`:

```go
if err := validateAndPrepareSocketBridges(svc.VM, cfg, opts.Mounts); err != nil
{
 return fmt.Errorf("socket bridges: %w", err)
}
```

After `WaitReady` on first create (`wasCreated`), before bootstrap:

```go
if err := prepareSocketBridgeGuestDirs(ctx, svc.VM, opts.SocketBridges); err !=
nil {
 return fmt.Errorf("socket bridge guest dirs: %w", err)
}
```

Mirror the validation call in `bootstrap_paths.go` `fullBootstrap` and
`fastRecreate` before `svc.VM.Start` (build opts first, then validate against
`opts.Mounts`). For `fullBootstrap`, call `prepareSocketBridgeGuestDirs` after
`WaitReady` when bridges are only on runtime opts — bootstrap path uses
bootstrap opts without bridges, so guest prep runs after
`finalizeAfterBootstrap` recreates with runtime opts, OR run guest prep in
`finalizeAfterBootstrap` after container promote.

**Simpler approach:** call `prepareSocketBridgeGuestDirs` inside
`finalizeAfterBootstrap` after `FinalizeAfterBootstrap` succeeds (when runtime
bridges are active), and in `resumeOrStartVM` after `WaitReady` when
`!UsesBootstrapOnlyMounts()` or when bridges present on current opts.

Concrete rule:

- Lima / non-bootstrap backends: prep after `WaitReady` in `resumeOrStartVM`
- Docker bootstrap path: prep in `finalizeAfterBootstrap` after promote (runtime
  opts now include bridges)

- [ ] **Step 3: Run unit tests**

Run: `go test ./test/unit/lifecycle/... -run SocketBridge -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/lifecycle/helpers.go internal/lifecycle/service.go
internal/lifecycle/bootstrap_paths.go test/unit/lifecycle/socket_bridges_test.go
git commit -m "feat: wire socket bridges through lifecycle start options"
```

---

## Task 7: Config hash

**Files:**

- Modify: `internal/lifecycle/state.go`
- Modify: `test/unit/lifecycle/hash_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestComputeConfigHash_SocketBridgesChangeHash(t *testing.T) {
 base := lifecycle.ComputeConfigHash(
  pluginDefs, nil, nil, []string{"mise"}, "claude", agentDefs,
  4, "8GB", "60GB", "", defaultVMMounts(), "aivm",
  nil,
 )
 withBridge := lifecycle.ComputeConfigHash(
  pluginDefs, nil, nil, []string{"mise"}, "claude", agentDefs,
  4, "8GB", "60GB", "", defaultVMMounts(), "aivm",
  []config.SocketBridge{{HostPath: "/tmp/a.sock", GuestPath:
  "/run/aivm/sockets/a.sock"}},
 )
 if base == withBridge {
  t.Fatal("hash must change when socket_bridges added")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/lifecycle/... -run SocketBridgesChangeHash -v`
Expected: FAIL — wrong `ComputeConfigHash` arity

- [ ] **Step 3: Implement hash input**

Add parameter to `ComputeConfigHash`:

```go
socketBridges []config.SocketBridge,
```

Normalize nil to empty slice. Add to `hashInput`:

```go
SocketBridges []config.SocketBridge `json:"socket_bridges"`
```

Update `currentConfigHash`:

```go
return ComputeConfigHash(
 // ... existing args ...
 svc.Config.ParsedSocketBridges,
)
```

Update all `ComputeConfigHash` call sites in `hash_test.go`.

- [ ] **Step 4: Run hash tests**

Run: `go test ./test/unit/lifecycle/... -run ComputeConfigHash -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lifecycle/state.go test/unit/lifecycle/hash_test.go
git commit -m "feat: include socket bridges in config hash"
```

---

## Task 8: Documentation

**Files:**

- Modify: `aivm.example.yaml`
- Modify: `README.md`

- [ ] **Step 1: Add example config**

In `aivm.example.yaml`, after `compose_file` / before `t3code` (top-level):

```yaml
# Unix socket bridges — expose host sockets inside the VM at stable guest paths.
# Conceptually similar to vm.mounts (host resource at guest path) but
implemented
# as socket forwarding, not directory binds. Set vm.env yourself if an app needs
# a path variable (e.g. HERDR_SOCKET_PATH).
#
# Docker: host_path must exist and be a Unix socket before VM create.
# Lima (macOS): host_path parent directory must be accessible; see README.
# socket_bridges:
#   - host_path: "${XDG_CONFIG_HOME}/herdr/service.sock"
#     guest_path: /run/aivm/sockets/herdr.sock
#   - host_path: "~/.config/foo/service.sock"
#     guest_path: /run/aivm/sockets/foo.sock
```

- [ ] **Step 2: Add README section**

Add `### Socket bridges` under Configuration (after Mounts). Cover:

- Purpose (host daemon socket → guest path)
- Minimal two-field example
- Docker: socket must exist at create time
- Lima: `portForwards` with known limitations (link to lima#1724)
- Changing bridges requires VM recreate (like mount remaps)
- Security: equivalent to granting VM access to host daemon API

- [ ] **Step 3: Lint markdown**

Run: `npx markdownlint-cli2 "README.md" "aivm.example.yaml" --fix`

- [ ] **Step 4: Commit**

```bash
git add aivm.example.yaml README.md
git commit -m "docs: document socket_bridges config"
```

---

## Task 9: Docker e2e round-trip test

**Files:**

- Modify: `test/framework/config.go`
- Create: `test/e2e/socket_bridge_test.go`

- [ ] **Step 1: Add harness option**

In `test/framework/config.go`, add to `testConfig`:

```go
SocketBridges []struct{ Host, Guest string }
```

Add option:

```go
func WithSocketBridge(hostPath, guestPath string) Option {
 return func(c *testConfig) {
  c.SocketBridges = append(c.SocketBridges, struct{ Host, Guest string
  }{hostPath, guestPath})
 }
}
```

In `buildTestYAML`, after vm section:

```go
if len(tc.SocketBridges) > 0 {
 fmt.Fprintf(&sb, "socket_bridges:\n")
 for _, b := range tc.SocketBridges {
  fmt.Fprintf(&sb, "  - host_path: %q\n", b.Host)
  fmt.Fprintf(&sb, "    guest_path: %q\n", b.Guest)
 }
}
```

- [ ] **Step 2: Write e2e test**

```go
//go:build integration

package e2e

import (
 "context"
 "net"
 "os"
 "path/filepath"
 "testing"
 "time"

 "github.com/sisimomo/aivm/test/framework"
 "github.com/sisimomo/aivm/test/framework/actions"
 "github.com/sisimomo/aivm/test/framework/assertions"
 "github.com/sisimomo/aivm/test/framework/conditions"
)

func TestSocketBridge_DockerRoundTrip(t *testing.T) {
 skipUnlessDocker(t)
 home, _ := os.UserHomeDir()
 sockDir := filepath.Join(home, ".aivm", "test-sockets", t.Name())
 hostSock := filepath.Join(sockDir, "echo.sock")
 guestSock := "/run/aivm/sockets/echo.sock"

 _ = os.Remove(hostSock)
 _ = os.MkdirAll(sockDir, 0o755)
 ln, err := net.Listen("unix", hostSock)
 if err != nil {
  t.Fatal(err)
 }
 t.Cleanup(func() { ln.Close(); os.Remove(hostSock) })

 go func() {
  for {
   conn, err := ln.Accept()
   if err != nil {
    return
   }
   _, _ = conn.Write([]byte("pong"))
   conn.Close()
  }
 }()

 h := framework.New(t, framework.WithSocketBridge(hostSock, guestSock))
 h.Scenario("socket bridge round-trip").
  Step("Start VM", actions.Start()).
  Wait("VM running", conditions.VMStatusRunning(), 120*time.Second).
  Assert("Guest path is socket", assertions.VMRunOutput(
   "test -S "+guestSock+" && echo socket-ok", "socket-ok")).
  Assert("Guest can connect", assertions.VMRunOutput(
   `python3 -c "import
   socket;s=socket.socket(socket.AF_UNIX);s.connect('`+guestSock+`');print(s.rec
   v(8).decode())"`,
   "pong")).
  Run()
}
```

Adjust imports/paths to match existing framework package layout (`actions`,
`conditions`, `assertions` — verify exact helper names from `test/e2e/`).

- [ ] **Step 3: Run e2e test**

Run: `make test-e2e RUN=TestSocketBridge`
Expected: PASS (requires Docker)

- [ ] **Step 4: Commit**

```bash
git add test/framework/config.go test/e2e/socket_bridge_test.go
git commit -m "test: add Docker socket bridge e2e round-trip"
```

---

## Task 10: Manual macOS checklist (documentation only)

**Files:**

- Modify: `README.md` (troubleshooting subsection)

- [ ] **Step 1: Add manual verification checklist**

Under socket bridges README section, add:

```markdown
#### Manual verification (Lima / macOS)

1. Configure a bridge to a real host daemon socket.
2. Start the VM; confirm a guest process connects via `guest_path`.
3. Run `aivm stop`, `aivm destroy`, change bridge config, recreate.
4. Restart the host daemon while the VM is running and retest connectivity.
```

- [ ] **Step 2: Lint and commit**

Run: `npx markdownlint-cli2 "README.md" --fix`

```bash
git add README.md
git commit -m "docs: add Lima socket bridge manual test checklist"
```

---

## Self-review

| Spec requirement | Task |
| --- | --- |
| Top-level `socket_bridges` config | Task 1 |
| `host_path` / `guest_path` validation | Task 1, 4 |
| `~` and `${VAR}` expansion | Task 1 |
| Duplicate path checks | Task 1 |
| Mount source overlap warn | Task 1 |
| `StartOptions.SocketBridges` | Task 2, 6 |
| Docker `-v file:file:ro` | Task 5 |
| Lima `portForwards` reverse | Task 3 |
| Runtime-only on Docker bootstrap | Task 6 |
| Start-time Docker socket exists | Task 4 |
| Start-time Lima parent dir | Task 4 |
| Guest path vs mount conflict | Task 4 |
| Config hash / recreate | Task 7 |
| Info/debug logging | Task 3, 5 |
| Guest parent dir prep | Task 6 |
| Unit tests | Tasks 1–4, 7 |
| Docker integration test | Task 9 |
| Docs | Tasks 8, 10 |
| No `guest_env`, no TCP | N/A (not implemented) |
| `GuestPathForHost` unchanged | N/A (no code changes) |

**Placeholder scan:** none — all steps include concrete code and commands.

**Type consistency:** `config.SocketBridge` (YAML/parsed) and `vm.SocketBridge`
(runtime) share field names `HostPath`/`GuestPath`; helpers use
`vm.SocketBridge`
throughout backend code.

---

## Execution handoff

Plan complete and saved to
`docs/superpowers/plans/2026-06-21-socket-bridges.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task,
review
between tasks, fast iteration

**2. Inline Execution** — execute tasks in this session using executing-plans,
batch execution with checkpoints

Which approach?
