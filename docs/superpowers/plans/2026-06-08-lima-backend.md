# Lima Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Colima VM backend with native Lima, add an opt-in `docker`
plugin for in-VM containers, and remove all Colima code paths.

**Architecture:** New `LimaVM` implements the existing `vm.VM` interface via
`limactl` lifecycle + SSH/scp (same patterns as today's Colima backend).
A bundled `lima.yaml` template provisions a plain Ubuntu VM with
`host.lima.internal` connectivity and no Docker socket forwarding. Docker
installs at bootstrap time only when listed in `plugins.enabled`.

**Tech Stack:** Go, Lima (`limactl`), OpenSSH, existing plugin/bootstrap system

**Spec:** `docs/superpowers/specs/2026-06-08-lima-backend-design.md`

---

## File map

| File | Responsibility |
| --- | --- |
| `internal/vm/lima.yaml` | Embedded Lima instance template (runtime source of truth) |
| `internal/vm/lima.go` | `LimaVM` — limactl lifecycle, script execution, cleanup |
| `internal/vm/template.go` | `go:embed` + temp-file helper for `limactl create` |
| `internal/vm/ssh.go` | `limaSSHEndpoint`, `InteractiveSSH` (Lima paths) |
| `internal/vm/factory.go` | Route `lima` / `docker` backends |
| `internal/plugin/defaults.yaml` | Opt-in `docker` plugin definition |
| `internal/compose/docker.go` | Host Docker socket discovery (no aivm exclusion) |
| `internal/t3code/tunnel.go` | SSH tunnel via `limaSSHEndpoint` |

---

## Task 1: SSH endpoint helper

**Files:**

- Modify: `internal/vm/ssh.go`
- Modify: `internal/vm/colima.go` (call sites only — updated again in Task 4)
- Create: `test/unit/vm/lima_ssh_test.go`

- [ ] **Step 1: Write failing test for Lima SSH coordinates**

```go
package vm_test

import (
 "os"
 "path/filepath"
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func TestLimaSSHEndpoint_DefaultHome(t *testing.T) {
 t.Setenv("LIMA_HOME", "")
 home, _ := os.UserHomeDir()

 cfg, host := vm.LimaSSHEndpoint("aivm")
 want := filepath.Join(home, ".lima", "aivm", "ssh.config")
 if cfg != want {
  t.Fatalf("ssh config: got %q want %q", cfg, want)
 }
 if host != "lima-aivm" {
  t.Fatalf("ssh host: got %q want lima-aivm", host)
 }
}

func TestLimaSSHEndpoint_LimaHome(t *testing.T) {
 t.Setenv("LIMA_HOME", "/custom/lima")
 cfg, host := vm.LimaSSHEndpoint("dev")
 if cfg != "/custom/lima/dev/ssh.config" {
  t.Fatalf("ssh config: got %q", cfg)
 }
 if host != "lima-dev" {
  t.Fatalf("ssh host: got %q", host)
 }
}
```

Export `LimaSSHEndpoint` from `internal/vm/ssh.go` (rename from
`colimaSSHEndpoint`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/... -run TestLimaSSHEndpoint -v`
Expected: FAIL — `LimaSSHEndpoint` undefined

- [ ] **Step 3: Implement `LimaSSHEndpoint`**

Replace `colimaSSHEndpoint` in `internal/vm/ssh.go`:

```go
// LimaSSHEndpoint returns the ssh/scp config file path and SSH hostname for a
// Lima instance. Written by limactl at VM creation time.
func LimaSSHEndpoint(profile string) (sshConfig, sshHost string) {
 home, _ := os.UserHomeDir()
 limaHome := os.Getenv("LIMA_HOME")
 if limaHome == "" {
  limaHome = filepath.Join(home, ".lima")
 }
 sshConfig = filepath.Join(limaHome, profile, "ssh.config")
 sshHost = "lima-" + profile
 return
}
```

Update `InteractiveSSH` to call `LimaSSHEndpoint`. Leave `colima.go` calling
the old name until Task 4 removes it — add a temporary alias if needed:

```go
func colimaSSHEndpoint(profile string) (string, string) {
 return LimaSSHEndpoint(profile)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/vm/... -run TestLimaSSHEndpoint -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/ssh.go test/unit/vm/lima_ssh_test.go
git commit -m "refactor(vm): add LimaSSHEndpoint helper"
```

---

## Task 2: Lima template

**Files:**

- Create: `internal/vm/lima.yaml`
- Create: `internal/vm/template.go`

- [ ] **Step 1: Create embedded Lima template**

`internal/vm/lima.yaml`:

```yaml
# aivm Lima instance template — VM only, no Docker provision.
# Docker is opt-in via the docker plugin at bootstrap.

minimumLimaVersion: 1.0.0

ssh:
  forwardAgent: false

hostResolver:
  hosts:
    host.docker.internal: host.lima.internal

# No portForwards for docker.sock — Docker stays inside the VM only.
```

`internal/vm/template.go`:

```go
package vm

import (
 "fmt"
 "os"
)

//go:embed lima.yaml
var limaTemplate []byte

// LimaTemplatePath writes the embedded template to a temp file for limactl.
// Caller may remove the file after limactl create completes.
func LimaTemplatePath() (string, error) {
 f, err := os.CreateTemp("", "aivm-lima-*.yaml")
 if err != nil {
  return "", fmt.Errorf("create lima template temp file: %w", err)
 }
 if _, err := f.Write(limaTemplate); err != nil {
  _ = f.Close()
  _ = os.Remove(f.Name())
  return "", fmt.Errorf("write lima template: %w", err)
 }
 if err := f.Close(); err != nil {
  _ = os.Remove(f.Name())
  return "", err
 }
 return f.Name(), nil
}
```

- [ ] **Step 2: Verify embed compiles**

Run: `go build ./internal/vm/...`
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/vm/lima.yaml internal/vm/template.go
git commit -m "feat(vm): add embedded Lima instance template"
```

---

## Task 3: LimaVM status parsing (unit tests)

**Files:**

- Create: `internal/vm/lima_status.go`
- Create: `test/unit/vm/lima_status_test.go`

- [ ] **Step 1: Write failing tests for status parsing**

```go
package vm_test

import (
 "testing"

 "github.com/sisimomo/aivm/internal/vm"
)

func TestParseLimaListStatus(t *testing.T) {
 t.Parallel()
 cases := []struct {
  name    string
  lines   []string
  profile string
  want    vm.Status
 }{
  {
   name: "running",
   lines: []string{
    "NAME       STATUS     SSH            VMTYPE    ARCH",
    "aivm       Running    127.0.0.1:22   vz        aarch64",
   },
   profile: "aivm",
   want:    vm.StatusRunning,
  },
  {
   name: "stopped",
   lines: []string{
    "aivm       Stopped    127.0.0.1:22   vz        aarch64",
   },
   profile: "aivm",
   want:    vm.StatusStopped,
  },
  {
   name:    "not found",
   lines:   []string{"other      Running"},
   profile: "aivm",
   want:    vm.StatusNotFound,
  },
 }
 for _, tc := range cases {
  t.Run(tc.name, func(t *testing.T) {
   got := vm.ParseLimaListStatus(tc.lines, tc.profile)
   if got != tc.want {
    t.Fatalf("got %v want %v", got, tc.want)
   }
  })
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/... -run TestParseLimaListStatus -v`
Expected: FAIL

- [ ] **Step 3: Implement parser**

`internal/vm/lima_status.go`:

```go
package vm

import "strings"

// ParseLimaListStatus maps limactl list output to Status for the given profile.
func ParseLimaListStatus(lines []string, profile string) Status {
 for _, line := range lines {
  fields := strings.Fields(line)
  if len(fields) < 2 || fields[0] != profile {
   continue
  }
  switch fields[1] {
  case "Running":
   return StatusRunning
  case "Stopped":
   return StatusStopped
  }
 }
 return StatusNotFound
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./test/unit/vm/... -run TestParseLimaListStatus -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/lima_status.go test/unit/vm/lima_status_test.go
git commit -m "feat(vm): add limactl list status parser"
```

---

## Task 4: LimaVM implementation

**Files:**

- Create: `internal/vm/lima.go`
- Remove: `internal/vm/colima.go`
- Modify: `internal/vm/ssh.go` (remove `colimaSSHEndpoint` alias)
- Modify: `internal/vm/vm.go` (update comments)

- [ ] **Step 1: Implement `LimaVM`**

Create `internal/vm/lima.go` by adapting `colima.go`:

Key differences from Colima:

| Colima | Lima |
| --- | --- |
| `colima list` | `limactl list` + `ParseLimaListStatus` |
| `colima start PROFILE` (resume) | `limactl start PROFILE` |
| `colima start PROFILE --cpu …` (create) | `limactl create TEMPLATE --name PROFILE --cpus … --memory … --disk … --mount …` then remove temp template |
| `colima stop PROFILE` | `limactl stop PROFILE` |
| `colima delete PROFILE --force --data` | `limactl delete PROFILE --force` |
| `colima ssh --profile P -- …` | `limactl shell P -- …` |
| `colimaSSHEndpoint` | `LimaSSHEndpoint` |

Create flow:

```go
templatePath, err := LimaTemplatePath()
if err != nil { return err }
defer os.Remove(templatePath)

args := []string{
 "create", templatePath,
 "--name", l.profile,
 "--cpus", strconv.Itoa(opts.CPUs),
 "--memory", strconv.Itoa(int(opts.MemoryBytes >> 30)),
 "--disk", strconv.Itoa(int(opts.DiskBytes >> 30)),
}
args = append(args, l.vmTypeFlags(opts.VMType)...)
for _, m := range opts.Mounts {
 flag := m.HostPath + ":r"
 if m.Writable {
  flag = m.HostPath + ":w"
 }
 args = append(args, "--mount", flag)
}
cmd := exec.CommandContext(ctx, "limactl", args...)
return aivmlog.RunCmd(cmd, "lima")
```

Stop/destroy docker cleanup script (conditional):

```go
const stopContainersScript = `command -v docker >/dev/null 2>&1 && \
  docker ps -q 2>/dev/null | xargs -r docker stop --time=10 2>/dev/null || true`
```

Copy `vmTypeFlags`, `shellescape`, `Run`/`RunOutput`/`RunStream`/`RunInteractive`/
`SSH`/`CopyTo`/`CopyFrom`/`WaitReady`/`AgeFile` from `colima.go`, replacing
`colima` with `limactl`/`LimaSSHEndpoint` as above.

- [ ] **Step 2: Delete `internal/vm/colima.go`**

- [ ] **Step 3: Update comments in `internal/vm/vm.go`**

Replace Colima references with Lima in `NeedsPortBindingAtBoot` and
`GetPublishedPort` doc comments.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: FAIL on factory/t3code until Task 5–6 — that's OK for this step;
fix any compile errors within vm package only.

Run: `go build ./internal/vm/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/
git commit -m "feat(vm): add LimaVM backend, remove ColimaVM"
```

---

## Task 5: Factory and config validation

**Files:**

- Modify: `internal/vm/factory.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/defaults.yaml`
- Modify: `internal/t3code/tunnel.go`
- Create: `test/unit/vm/factory_test.go`
- Create: `test/unit/config/backend_test.go`

- [ ] **Step 1: Write failing factory test**

```go
package vm_test

import (
 "testing"

 "github.com/sisimomo/aivm/internal/config"
 "github.com/sisimomo/aivm/internal/vm"
)

func TestNewFromConfig_Lima(t *testing.T) {
 cfg := &config.VMConfig{Backend: "lima", Name: "aivm"}
 inst, err := vm.NewFromConfig(cfg, "/tmp/state")
 if err != nil {
  t.Fatal(err)
 }
 if inst.Profile() != "aivm" {
  t.Fatalf("profile: got %q", inst.Profile())
 }
}

func TestNewFromConfig_RejectsColima(t *testing.T) {
 cfg := &config.VMConfig{Backend: "colima", Name: "aivm"}
 _, err := vm.NewFromConfig(cfg, "/tmp/state")
 if err == nil {
  t.Fatal("expected error for colima backend")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/unit/vm/... -run TestNewFromConfig -v`
Expected: FAIL

- [ ] **Step 3: Update factory**

`internal/vm/factory.go`:

```go
// NewFromConfig constructs the appropriate VM backend from the given VM config.
// The backend field selects the implementation; "lima" (default) creates a
// LimaVM, "docker" creates a DockerVM.
func NewFromConfig(cfg *config.VMConfig, stateDir string) (VM, error) {
 switch cfg.Backend {
 case "", "lima":
  return NewLima(cfg.Profile(), stateDir), nil
 case "docker":
  return NewDocker(cfg.Profile(), stateDir, cfg.DockerImage), nil
 default:
  return nil, fmt.Errorf("unknown vm backend %q", cfg.Backend)
 }
}
```

`internal/config/config.go` — backend validation:

```go
switch vm.Backend {
case "", "lima", "docker":
 // valid
default:
 return fmt.Errorf(
  "vm.backend: unsupported value %q — use \"lima\" or \"docker\"",
  vm.Backend,
 )
}

if (vm.Backend == "" || vm.Backend == "lima") && vm.Name == "" {
 return fmt.Errorf("vm.name: must not be empty when using the lima backend")
}
```

Update `Name` field comment:

`// VM identity (Lima instance name / Docker container name)`

`internal/config/defaults.yaml`:

```yaml
vm:
  backend: lima
```

`internal/t3code/tunnel.go` — replace `sshCoords()` body:

```go
func (t *Tunnel) sshCoords() (sshConfig, sshHost string) {
 return vm.LimaSSHEndpoint(t.Profile)
}
```

Remove the `colimaHome` / `COLIMA_HOME` logic and unused imports.

- [ ] **Step 4: Write config validation test**

`test/unit/config/backend_test.go`:

```go
package config_test

import (
 "strings"
 "testing"

 "github.com/sisimomo/aivm/internal/config"
)

func TestValidateVMConfig_RejectsColima(t *testing.T) {
 cfg := config.VMConfig{
  Backend: "colima", Name: "aivm",
  CPUs: 1, MemoryBytes: 1, DiskBytes: 1,
 }
 err := config.ValidateVMConfig(&cfg)
 if err == nil || !strings.Contains(err.Error(), "colima") {
  t.Fatalf("want colima rejection, got %v", err)
 }
}
```

Export `ValidateVMConfig` only if not already exported — if validation is
private, test via `config.Load` with a temp yaml file instead:

```go
err := config.Load("/path/to/temp.yaml")
```

Use whichever pattern exists in `test/unit/config/`.

- [ ] **Step 5: Run tests**

Run: `go test ./test/unit/vm/... ./test/unit/config/... -v`
Expected: PASS

Run: `go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/vm/factory.go internal/config/ internal/t3code/tunnel.go test/unit/
git commit -m "feat(config): default to lima backend, remove colima"
```

---

## Task 6: Docker plugin (opt-in)

**Files:**

- Modify: `internal/plugin/defaults.yaml`
- Modify: `test/unit/plugin/defaults_test.go`
- Create: `test/bootstrap/docker_test.go`

- [ ] **Step 1: Add docker plugin to defaults.yaml**

Append after `awscli`:

```yaml
docker:
  description: "Docker Engine (rootful) inside the VM"
  dependencies:
    - system
  setup: |
    curl -fsSL https://get.docker.com | sh
    sudo systemctl enable --now docker
    sudo usermod -aG docker "$USER"
```

- [ ] **Step 2: Update defaults_test.go**

Add to `TestLoadDefaults_AllPluginsPresent` cases — **do not** add `docker`
to the required-present list (it's opt-in, not always enabled). Add a new test:

```go
func TestLoadDefaults_DockerDependsOnSystem(t *testing.T) {
 defs, err := plugin.LoadDefaults()
 if err != nil {
  t.Fatal(err)
 }
 docker, ok := defs["docker"]
 if !ok {
  t.Fatal("docker plugin not found")
 }
 if docker.Setup == "" {
  t.Fatal("docker plugin missing setup")
 }
 found := false
 for _, dep := range docker.Dependencies {
  if dep == "system" {
   found = true
  }
 }
 if !found {
  t.Fatal("docker must depend on system")
 }
}
```

- [ ] **Step 3: Run plugin unit tests**

Run: `go test ./test/unit/plugin/... -v`
Expected: PASS

- [ ] **Step 4: Add bootstrap test (skipped in Docker harness)**

`test/bootstrap/docker_test.go`:

```go
//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Docker installs rootful Docker inside a VM with systemd.
// The bootstrap harness uses a plain Docker container without systemd, so this
// test is skipped here; full install is verified manually on macOS + Lima.
func TestPlugin_Docker(t *testing.T) {
 t.Skip("docker plugin requires systemd (Lima VM); see manual checklist in spec")
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/defaults.yaml test/unit/plugin/defaults_test.go test/bootstrap/docker_test.go
git commit -m "feat(plugin): add opt-in docker plugin for in-VM containers"
```

---

## Task 7: Compose socket discovery

**Files:**

- Modify: `internal/compose/docker.go`
- Modify: `cmd/aivm/main.go` (comment only)

- [ ] **Step 1: Simplify `FindHostDockerSocket`**

Remove the `colimaProfile` parameter and aivm-socket exclusion logic.
Rename to reflect purpose; update call site in `cmd/aivm/main.go`:

```go
// FindHostDockerSocket probes for a host-side Docker runtime for compose_file.
func FindHostDockerSocket(ctx context.Context) (string, error) {
 home, _ := os.UserHomeDir()

 currentCtx, _ := run.Output(ctx, "docker", "context", "show")
 if currentCtx != "" {
  sockURI, _ := run.Output(ctx, "docker", "context", "inspect", currentCtx,
   "--format", "{{.Endpoints.docker.Host}}")
  if sockURI != "" {
   rawPath := strings.TrimPrefix(sockURI, "unix://")
   if fi, err := os.Stat(rawPath); err == nil &&
    (fi.Mode()&os.ModeSocket != 0) {
    return sockURI, nil
   }
  }
 }

 if fi, err := os.Stat("/var/run/docker.sock"); err == nil &&
  (fi.Mode()&os.ModeSocket != 0) {
  return "unix:///var/run/docker.sock", nil
 }

 orbSock := filepath.Join(home, ".orbstack", "run", "docker.sock")
 if fi, err := os.Stat(orbSock); err == nil && (fi.Mode()&os.ModeSocket != 0) {
  return "unix://" + orbSock, nil
 }

 defaultSock := filepath.Join(home, ".colima", "default", "docker.sock")
 if fi, err := os.Stat(defaultSock); err == nil &&
  (fi.Mode()&os.ModeSocket != 0) {
  return "unix://" + defaultSock, nil
 }

 return "", fmt.Errorf(`no suitable host Docker runtime found.
Compose services require a Docker runtime on the host (separate from the aivm VM).
Options:
  • Docker Desktop:  https://www.docker.com/products/docker-desktop/
  • OrbStack:        https://orbstack.dev/
  • Colima:          colima start`)
}
```

`cmd/aivm/main.go`:

```go
dockerHostProbe, err := compose.FindHostDockerSocket(context.Background())
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/compose/docker.go cmd/aivm/main.go
git commit -m "refactor(compose): drop colima profile exclusion from socket probe"
```

---

## Task 8: Log tags and stray Colima references

**Files:**

- Modify: `test/unit/log/file_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/lifecycle/service.go` (comments)
- Modify: `internal/lifecycle/helpers.go` (comments)
- Modify: `internal/vm/vm.go` (if not done)
- Remove: `config/colima.yaml`
- Modify: `Makefile` (comment if present)

- [ ] **Step 1: Update log unit tests**

Replace `[colima]` with `[lima]` and `Writer("colima")` with `Writer("lima")`
in `test/unit/log/file_test.go`.

- [ ] **Step 2: Update CLI and comments**

`internal/cli/root.go`:

```go
Short: "Launch AI agents in a secure Lima VM",
Long: `Launch AI agents in a secure, disposable Lima VM.`,
```

Grep for remaining `colima` / `Colima` references:

Run: `rg -i colima --glob '!docs/**' --glob '!.git/**'`
Expected: zero matches (or only historical comments in git-ignored paths)

Fix each hit.

- [ ] **Step 3: Delete `config/colima.yaml`**

- [ ] **Step 4: Run tests**

Run: `go test ./test/unit/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove remaining Colima references and config"
```

---

## Task 9: Documentation

**Files:**

- Modify: `README.md`
- Modify: `aivm.example.yaml`

- [ ] **Step 1: Update README**

- Requirements: replace Colima with Lima (`brew install lima`)
- VM backend table: `lima` default, remove `colima` row
- Add `docker` to plugins table: "Docker Engine inside the VM (opt-in)"
- Note: `compose_file` still requires separate host Docker
- Note: enable in-VM Docker with `plugins.enabled: [docker]`

- [ ] **Step 2: Update aivm.example.yaml**

```yaml
vm:
  backend: lima
```

Add commented example:

```yaml
# plugins:
#   enabled:
#     - system
#     - docker   # opt-in: rootful Docker inside the VM
```

- [ ] **Step 3: Lint markdown**

Run: `npx markdownlint-cli2 "README.md" --fix`

- [ ] **Step 4: Commit**

```bash
git add README.md aivm.example.yaml
git commit -m "docs: Lima backend and opt-in docker plugin"
```

---

## Task 10: Final verification

- [ ] **Step 1: Run full unit test suite**

Run: `go test ./test/unit/... -v`
Expected: all PASS

- [ ] **Step 2: Run linter on edited Go files**

Run: `golangci-lint run ./internal/vm/... ./internal/config/... \
  ./internal/compose/... ./internal/t3code/... ./internal/plugin/...`
Expected: PASS

- [ ] **Step 3: Format Go**

Run: `go fmt ./...`

- [ ] **Step 4: Manual macOS checklist** (on developer machine with Lima)

1. `brew install lima`
2. `aivm destroy --force` (ignore errors)
3. `limactl delete aivm --force` (clean stale)
4. `aivm start` — Lima VM created, no `~/.lima/aivm/sock/docker.sock`
5. `aivm ssh` → `command -v docker` fails (not installed)
6. Add `docker` to `plugins.enabled`, `aivm` → bootstrap runs
7. `aivm ssh` → `docker ps` works
8. `curl http://host.lima.internal:<host-port>` from VM reaches host service
9. `aivm stop` — clean shutdown with docker plugin enabled
10. Remove `docker` from plugins, `aivm stop` — clean shutdown without docker

- [ ] **Step 5: Final commit if any fixups**

```bash
git add -A
git commit -m "fix: address review feedback from lima backend migration"
```

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| Replace colima with lima backend | 4, 5 |
| Opt-in docker plugin | 6 |
| No host docker.sock | 2 (no portForwards) |
| host.lima.internal / host.docker.internal | 2 |
| Host-side compose unchanged | 7 |
| Conditional docker stop on shutdown | 4 |
| LIMA_HOME support | 1 |
| T3 SSH tunnel | 5 |
| Breaking: no colima backend | 5 |
| Docs + example config | 9 |
| Unit tests | 1, 3, 5, 6, 8 |
| Manual macOS verification | 10 |
