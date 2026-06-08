# Lima Backend Design

## Summary

Replace the Colima VM backend with a native Lima backend. aivm gets a Linux VM
on macOS via `limactl`, with rootful Docker installed inside the VM but no
Docker socket exposed to the host. The existing `VM` interface, SSH-first
execution model, and host-side `compose_file` feature are preserved.

This is a breaking change with no migration path — acceptable in alpha.

## Goals

- Replace `vm.backend: colima` with `vm.backend: lima` (new default)
- Provision rootful Docker inside the VM for agent/container workloads
- Do **not** forward the Docker socket to the host (no `sock/docker.sock`)
- Preserve VM → host connectivity (`host.lima.internal`, `host.docker.internal`
  inside containers)
- Preserve host → VM connectivity (mounts, SSH port forwards for T3 Code)
- Keep host-side `compose_file` unchanged (separate host Docker runtime)
- Keep `docker` backend for CI/tests

## Non-Goals

- Colima deprecation period, migration tooling, or upgrade warnings
- Moving `compose_file` services inside the aivm VM
- Rootless Docker inside the VM
- Host-side `docker` CLI access to the aivm VM daemon
- Linux/Windows host support (macOS remains the only supported platform)
- Lima e2e tests in CI (manual verification on macOS only, same as Colima today)

## Architecture

```
Host (macOS)
├── aivm CLI
│   ├── LimaVM ──limactl──► ~/.lima/<name>/  (VM)
│   │                         ├── SSH ──► Run, RunStream, bootstrap
│   │                         ├── scp ──► CopyTo, CopyFrom
│   │                         └── ssh -L ──► T3 Code tunnel
│   └── ComposeManager ──docker compose──► Host Docker (separate runtime)
│
└── No docker.sock for aivm VM on host
```

### Removed

- `internal/vm/colima.go`
- `config/colima.yaml`
- All `colima` CLI invocations and Colima-specific path helpers
- `vm.backend: colima` config value

### Added

- `internal/vm/lima.go` — `LimaVM` implementing `vm.VM`
- `config/lima.yaml` — bundled Lima instance template
- Shared SSH helper (`limaSSHEndpoint`) used by `vm`, `t3code`

### Unchanged

- `vm.VM` interface
- `DockerVM` backend (`vm.backend: docker`) for CI/tests
- Host-side `compose_file` lifecycle (`internal/compose/`)
- Bootstrap, plugins, agents, idle monitor, session management

## Lima Template (`config/lima.yaml`)

Bundled base template shipped with aivm. Lima merges this with CLI flags at
instance creation; the combined config is stored at
`$LIMA_HOME/<name>/lima.yaml`.

### Base settings

| Setting | Value |
| --- | --- |
| Base image | Ubuntu (Lima default) |
| VM type | Set at create time: `vz` + Rosetta on darwin/arm64; `qemu` otherwise |
| SSH agent forwarding | `false` |
| Mount type | Lima default (`virtiofs` on vz, `reverse-sshfs` on qemu) |

### Docker provisioning (rootful, in-VM only)

Provision scripts install Docker via `get.docker.com` and enable the systemd
unit. Modeled on Lima's `docker-rootful` template **without** the socket
forward block:

```yaml
provision:
  - mode: system
    script: |
      #!/bin/bash
      set -eux -o pipefail
      command -v docker >/dev/null 2>&1 && exit 0
      curl -fsSL https://get.docker.com | sh
      systemctl enable --now docker
```

No `portForwards` entry mapping `guestSocket: /var/run/docker.sock` to a
host socket. Docker is reachable only from inside the VM (via SSH/shell).

### Host connectivity

VM processes reach the macOS host at `host.lima.internal` (Lima default
user-mode network, gateway `192.168.5.2`). For Docker containers spawned
inside the VM:

```yaml
hostResolver:
  hosts:
    host.docker.internal: host.lima.internal
```

This ensures `host.docker.internal:PORT` works from containers (e.g. an agent
running `docker run` that needs a host API on `localhost:3000`).

### Runtime flags (passed by `LimaVM.Start` at create)

Mirrors current Colima `Start` behavior:

| Flag | Source |
| --- | --- |
| `--cpus` | `vm.cpus` |
| `--memory` | `vm.memory` (GiB) |
| `--disk` | `vm.disk` (GiB) |
| `--mount <path>:w` / `:r` | `vm.mounts` + agent persist dirs + T3 dir |
| `--vm-type vz --rosetta` | `vm.type` or auto (darwin/arm64 → vz) |
| `--vm-type qemu` | non-Apple-Silicon or explicit `vm.type: qemu` |
| `--name` | `vm.name` (profile) |

On resume of a stopped instance: `limactl start <name>` only. Mounts and
resources are baked in at creation (same constraint as Colima today).

## `LimaVM` Implementation

New file `internal/vm/lima.go`, structurally parallel to the removed
`colima.go`.

| `VM` method | Lima implementation |
| --- | --- |
| `Status` | `limactl list` (parse name + status) |
| `Start` (new) | `limactl create <template> --name … --mount … --cpus …` |
| `Start` (stopped) | `limactl start <name>` |
| `Stop` | `docker stop` inside VM, then `limactl stop <name>` |
| `Destroy` | `limactl delete <name>` |
| `Run` / `RunOutput` | `limactl shell <name> -- bash -lc …` (base64 script, same as Colima) |
| `RunStream` | `ssh -F <ssh.config> lima-<name> bash -lc …` |
| `RunInteractive` / `SSH` | `ssh -t -F <ssh.config> lima-<name> …` (PTY for TUIs) |
| `CopyTo` / `CopyFrom` | `scp -F <ssh.config> …` |
| `WaitReady` | SSH probe: `echo ready` |
| `NeedsPortBindingAtBoot` | `false` |
| `GetPublishedPort` | Return `containerPort` unchanged (SSH tunnel model) |

### SSH coordinates

Refactor `colimaSSHEndpoint` → `limaSSHEndpoint`:

```go
// $LIMA_HOME defaults to ~/.lima
sshConfig = filepath.Join(limaHome, profile, "ssh.config")
sshHost   = "lima-" + profile
```

Respect `LIMA_HOME` environment variable (analogous to today's `COLIMA_HOME`).

Update `internal/t3code/tunnel.go` to use the shared helper.

### Lifecycle lock

Reuse existing `LifecycleLock` pattern from ColimaVM (same `stateDir`).

### Logging

Subprocess log tag changes from `[colima]` to `[lima]` in `aivmlog.RunCmd`
calls. Update unit tests in `test/unit/log/file_test.go` accordingly.

## Configuration Changes

### Defaults

`internal/config/defaults.yaml`:

```yaml
vm:
  backend: lima
```

### Validation

`internal/config/config.go`:

- Accept: `""`, `"lima"`, `"docker"`
- Reject: `"colima"` (and any unknown value)
- `vm.name` required when backend is `lima` (same rule as colima today)

### User config

```yaml
vm:
  backend: lima
  name: aivm
  cpus: 4
  memory: "8GB"
  disk: "60GB"
  mounts:
    - "~/dev:rw"
```

No new config keys. `vm.type` continues to control vz/qemu selection.

## Compose (unchanged)

`compose_file` continues to run `docker compose` on a **separate host Docker**
runtime (Docker Desktop, OrbStack, or any non-aivm Colima profile).

`FindHostDockerSocket` simplifies: there is no aivm VM socket to exclude.
Remove the Colima-profile socket skip logic; probe host runtimes as today.

## Factory

`internal/vm/factory.go`:

```go
switch cfg.Backend {
case "", "lima":
    return NewLima(cfg.Profile(), stateDir), nil
case "docker":
    return NewDocker(...)
}
```

## Documentation Updates

- README: replace Colima prerequisite with Lima (`brew install lima`)
- README: update VM backend table (`lima` default, remove `colima`)
- CLI help text: "secure Lima VM" instead of "secure Colima VM"
- Remove `config/colima.yaml`, add `config/lima.yaml` with comments
- `aivm.example.yaml`: `backend: lima`

## Breaking Changes (alpha)

- `vm.backend: colima` is removed; configs must use `lima`
- Existing Colima VMs (`~/.colima/<name>/`) are not migrated or cleaned up
- Users recreate via `aivm destroy` + `aivm start`, or manually delete the
  old Colima profile
- No deprecation warnings or migration guide beyond the git commit

## Testing

### Unit tests

- `LimaVM` status parsing from `limactl list` output
- `limaSSHEndpoint` path construction (`LIMA_HOME`, default `~/.lima`)
- Factory routes `lima` and rejects `colima`
- Log writer tag tests updated to `[lima]`

### E2E / integration

- Existing e2e tests use `docker` backend — no change required
- Manual macOS verification checklist:
  - `aivm start` creates Lima instance
  - `aivm ssh` → `docker ps` works inside VM
  - `curl host.lima.internal:<port>` reaches a host service
  - `docker run` with `host.docker.internal` reaches host from container
  - No `~/.lima/<name>/sock/docker.sock` created
  - T3 Code SSH tunnel works when enabled
  - `compose_file` still works with a separate host Docker

## File Change Summary

| Action | Path |
| --- | --- |
| Add | `internal/vm/lima.go` |
| Add | `config/lima.yaml` |
| Add | `docs/superpowers/specs/2026-06-08-lima-backend-design.md` |
| Remove | `internal/vm/colima.go` |
| Remove | `config/colima.yaml` |
| Modify | `internal/vm/factory.go` |
| Modify | `internal/vm/ssh.go` (rename helper, Lima paths) |
| Modify | `internal/t3code/tunnel.go` |
| Modify | `internal/compose/docker.go` |
| Modify | `internal/config/config.go` |
| Modify | `internal/config/defaults.yaml` |
| Modify | `internal/cli/root.go` |
| Modify | `cmd/aivm/main.go` (comments only) |
| Modify | `README.md`, `aivm.example.yaml` |
| Modify | `test/unit/log/file_test.go` |
