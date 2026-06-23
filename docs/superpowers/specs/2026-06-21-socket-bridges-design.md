# Socket Bridges Design

## Summary

Add a generic, config-driven way to expose host Unix domain sockets inside the
AIVM VM at stable guest paths. Apps in the VM connect to a normal local socket;
they do not need to know about TCP, ports, or backend networking.

Each backend uses its **native socket-forwarding mechanism** — no custom TCP
proxy, no guest forwarder daemon, no auto-allocated ports:

| Backend | Mechanism |
| --- | --- |
| **Docker** | Bind mount (`-v host_path:guest_path:ro`) |
| **Lima** | `portForwards` (`guestSocket` + `hostSocket`, `reverse: true`) |

Public config stays minimal: two fields per bridge (`host_path`, `guest_path`).
This is **not** folded into `vm.mounts` — same user goal (host resource visible
in the VM at a guest path), different semantics and backend wiring.

## Goals

- Host socket → guest socket, transparent to applications
- Minimal public config (two fields per bridge; no ports, bind addresses, or
  `guest_env`)
- Generic infrastructure — not Herdr/MCP/agent-specific
- Lima (macOS) and Docker (CI/tests) both supported
- Backend-native implementation (bind mount / Lima port forward), no TCP hop
- Lifecycle integrated with VM create/recreate (same class of change as mounts)

## Non-Goals

- TCP ports, bind addresses, or retry knobs in user config
- `guest_env` in bridge config (users set `vm.env` if an app needs a path var)
- Custom host/guest forwarder binaries or supervisors
- Bridging host TCP services (Unix sockets only)
- File-level or directory `vm.mounts` changes (directories stay in `vm.mounts`)
- Path translation for `aivm ssh` / agent launch (`GuestPathForHost` unchanged)
- Linux/Windows **host** support beyond existing AIVM platform scope
- Lima socket-bridge e2e in CI (manual macOS checklist; Docker integration tests)

## Background

Example use case: a daemon on the macOS host listens on
`~/.config/foo/service.sock`; agents in the VM should talk to
`/run/aivm/sockets/foo.sock` as if it were local.

Unix domain sockets cannot cross OS/VM boundaries via a plain filesystem bind
(e.g. macOS host socket visible inside a Linux Lima guest). Backends therefore
use purpose-built socket forwarding:

- **Docker (Linux host + Linux container):** same kernel — bind-mount the socket
  file into the container.
- **Lima (macOS host + Linux guest):** Lima `portForwards` with Unix socket
  fields forwards between host and guest socket paths.

Neither approach requires AIVM to run a TCP listener or a guest-side proxy.

## Why not `vm.mounts`?

`vm.mounts` is the directory mount system ([mount system design](./2026-06-12-aivm-mount-system-design.md)):

- Directories only; overlap and prefix rules; `GuestPathForHost` translation
- Lima implements mounts as virtiofs/reverse-sshfs **directory** binds
- Docker implements mounts as `-v` **directory** binds

Socket bridges share the *user mental model* (“host thing available at guest
path”) but differ in validation, lifecycle hooks, and backend wiring:

| Concern | `vm.mounts` | `socket_bridges` |
| --- | --- | --- |
| Resource type | Directory tree | Single Unix socket endpoint |
| Lima implementation | `mounts[]` in Lima template | `portForwards[]` in Lima template |
| Docker implementation | `-v dir:dir` | `-v file:file:ro` |
| CWD translation | Yes (`vm.mounts` only) | No |
| Overlap rules | Source prefix checks | Duplicate path checks only |

Keeping a dedicated top-level `socket_bridges` key makes intent explicit and
avoids overloading mount validation.

## Architecture

```text
aivm.yaml
  socket_bridges[]
        │
        ▼
  config load: validate, expand host_path, resolve paths
        │
        ▼
  buildStartOptions → StartOptions.SocketBridges[]
        │
   ┌────┴────┐
   │         │
 Docker     Lima
 -v bind    portForwards in generated lima.yaml
 (file)     (guestSocket + hostSocket, reverse: true)
        │
        ▼
  VM app connects to guest_path (local Unix socket)
```

```text
Host (macOS or Linux)
├── Host daemon listens on host_path (e.g. ~/.config/foo/service.sock)
│
VM
└── App connects to guest_path (e.g. /run/aivm/sockets/foo.sock)
         │
         └── backend forwards to host_path transparently
```

## Public config

Top-level key (alongside `compose_file`, `t3code`):

```yaml
socket_bridges:
  - host_path: "${HERDR_SOCKET}"          # literal or ${HOST_VAR} expansion
    guest_path: /run/aivm/sockets/foo.sock
```

| Field | Rules |
| --- | --- |
| `host_path` | Required. Expanded with `os.ExpandEnv` at load (same as `vm.env`). `~` expanded to host home. Must be absolute after expansion. Must refer to a Unix socket path (not a directory). |
| `guest_path` | Required. Absolute path in the VM. Recommended convention: `/run/aivm/sockets/<name>.sock`. |

**Not in public config:** `port`, `bind`, `guest_env`, timeouts, or retry policy.

Users who need env vars (e.g. `HERDR_SOCKET_PATH=/run/aivm/sockets/herdr.sock`)
set them in `vm.env` or `vm.session_env` themselves.

### Example

```yaml
socket_bridges:
  - host_path: "${XDG_CONFIG_HOME}/herdr/service.sock"
    guest_path: /run/aivm/sockets/herdr.sock
  - host_path: "~/.config/foo/service.sock"
    guest_path: /run/aivm/sockets/foo.sock

vm:
  env:
    HERDR_SOCKET_PATH: /run/aivm/sockets/herdr.sock
```

## Backend implementation

### Docker

Each bridge becomes a read-only bind mount at container create:

```text
-v <HostPath>:<GuestPath>:ro
```

Implemented by appending to the existing mount loop in `DockerVM.startFromImage`
(alongside directory mounts from `opts.Mounts`). Socket bridges are **not**
mixed into `vm.Mounts` assembly — they are a separate slice on `StartOptions`.

**Requirements:**

- `host_path` must exist and be a socket (`S_ISSOCK`) before `docker run`.
  If the path is missing, Docker creates an empty **directory** at that location
  on the host — a known Docker footgun. AIVM validates at VM create and fails
  with a clear error when the host socket is absent.
- Mounts are baked at container creation (`UsesBootstrapOnlyMounts`). Bridge
  changes require VM recreate (same as mount changes).
- Container user must have permission to access the host socket (mode/owner).
  Document in troubleshooting; no automatic chmod in v1.

When `socket_bridges` is non-empty and the container image lacks parent dirs for
`guest_path`, Docker creates the mount point path in the container filesystem as
part of the bind mount (same as any `-v file:path`).

### Lima

Each bridge becomes a `portForwards` entry in the generated Lima template
(written by `LimaTemplatePath`, alongside the existing `mounts:` section):

```yaml
portForwards:
  - guestSocket: "/run/aivm/sockets/foo.sock"
    hostSocket: "/Users/you/.config/foo/service.sock"
    reverse: true
```

- `guestSocket` = resolved `guest_path`
- `hostSocket` = resolved `host_path` (absolute macOS path)
- `reverse: true` — forward host socket into the guest (VM app dials
  `guest_path`; traffic reaches the host daemon on `host_path`)

**Requirements:**

- `portForwards` are baked at `limactl create`. Bridge changes require VM
  recreate (same as remapped directory mounts).
- Host user needs read/write/execute on the **directory containing** `host_path`
  (Lima requirement).
- Guest user needs read/write access to `guest_path` (Lima creates/forwards the
  guest socket per Lima rules).
- Prefer stable guest paths under `/run/aivm/sockets/` (create parent via
  bootstrap tmpfiles or first-start `mkdir` if Lima does not create it).

**Known Lima limitations (document, do not paper over):**

- Some `reverse: true` socket forward setups have reported host-socket deletion
  or instability ([lima#1724](https://github.com/lima-vm/lima/issues/1724)).
  Manual verification required for production host daemons.
- Lima recommends placing forwarded host sockets under `{{.Dir}}/sock` for
  internal services; user paths outside that tree are supported but may need
  extra care with permissions and restarts.

## Internal types

### Config (`internal/config/config.go`)

```go
type SocketBridge struct {
    HostPath  string `mapstructure:"host_path"`
    GuestPath string `mapstructure:"guest_path"`
}

// On Config (top-level):
SocketBridges []SocketBridge `mapstructure:"socket_bridges"`
```

```go
// ResolvedSocketBridges returns bridges with host_path expanded (~ and ${VAR}).
func (c *Config) ResolvedSocketBridges() ([]SocketBridge, error)
```

### VM layer (`internal/vm/vm.go`)

```go
type SocketBridge struct {
    HostPath  string
    GuestPath string
}

type StartOptions struct {
    // ... existing fields ...
    SocketBridges []SocketBridge
}
```

No new `vm.VM` interface methods — backends read `StartOptions.SocketBridges`
during `Start` / template generation, consistent with backend encapsulation.

### Lima template helper (`internal/vm/template.go`)

Extend `LimaTemplatePath` to accept `[]SocketBridge` and append a `portForwards:`
section (mirroring `LimaMountsYAML`):

```go
func LimaTemplatePath(mounts []Mount, bridges []SocketBridge) (string, error)
func LimaPortForwardsYAML(bridges []SocketBridge) string
```

### Lifecycle (`internal/lifecycle/helpers.go`)

`buildRuntimeStartOptions` / `buildBootstrapStartOptions` populate
`StartOptions.SocketBridges` from `cfg.ResolvedSocketBridges()`.

Socket bridges apply at **runtime** profile (same as full mounts on Lima). On
Docker, attach bridges only in `buildRuntimeStartOptions` / post-bootstrap
`FinalizeAfterBootstrap` opts — not in `buildBootstrapStartOptions` — matching
when agent mounts are attached.

## Config load validation

| Rule | On violation |
| --- | --- |
| `host_path`, `guest_path` required | Config load error |
| Paths absolute after `~` and env expansion | Config load error |
| No duplicate `guest_path` | Config load error |
| No duplicate resolved `host_path` | Config load error |
| `host_path` equals a `vm.mounts` source | Warn only at load — unlikely to work as intended |
| Empty `socket_bridges` | Valid (feature disabled) |

Validation at **VM create** (start path):

| Rule | On violation |
| --- | --- |
| Docker: `host_path` exists and is a socket | Fail start with clear message |
| Lima: directory of `host_path` accessible | Fail start with clear message |
| Guest `guest_path` not already used by a directory mount target | Fail start |

## Lifecycle and config hash

Socket bridges are **execution-relevant** configuration (like `vm.mounts`):

- Include resolved `socket_bridges` in `ComputeConfigHash` input
- Changing bridges triggers the existing config-changed → recreate prompt flow
- Do **not** add a separate lightweight sync path (unlike `vm.env`)
- No host-side port state file; no PID files for forwarders

**Start / Stop / Destroy:** no extra steps beyond VM create/recreate. Backends
own forwarding for the VM lifetime.

**Bootstrap guest prep (optional, idempotent):** ensure `/run/aivm/sockets`
exists in the guest (tmpfiles.d or `mkdir` in bootstrap) so Lima guest socket
paths have a stable parent directory.

## Error handling

| Situation | Behavior |
| --- | --- |
| Missing `host_path` at Docker create | Fail start; do not run `docker create` |
| Missing `host_path` at Lima create | Warn; Lima forward may fail until socket appears |
| Host daemon restarts and recreates socket | Docker: bind mount tracks same path if recreated at same path; Lima: depends on Lima forwarder behavior — document restart testing |
| Duplicate `guest_path` in config | Config load error |
| Bridge config changed | Config hash mismatch → recreate prompt |
| CWD under socket path | Not supported; `AssertUnderMount` ignores bridges |

## Observability

- Info log at VM create: one line per bridge
  (`guest_path ← host_path (docker bind)` / `(lima portForward)`)
- Debug log: full resolved paths
- No ports or bind addresses in default output

## Documentation updates

| File | Change |
| --- | --- |
| `aivm.example.yaml` | Commented `socket_bridges` example |
| `README.md` | Short section: purpose, example, backend note, Docker socket-must-exist |

Explain that socket bridges are conceptually like mounts (host → guest path) but
configured separately because backends implement them as socket forwarding, not
directory binds.

## File change summary

| Action | Path |
| --- | --- |
| Add | `docs/superpowers/specs/2026-06-21-socket-bridges-design.md` |
| Modify | `internal/config/config.go` — `SocketBridge`, validation, resolve |
| Modify | `internal/vm/vm.go` — `StartOptions.SocketBridges` |
| Modify | `internal/vm/template.go` — `LimaPortForwardsYAML`, extend `LimaTemplatePath` |
| Modify | `internal/vm/lima.go` — pass bridges into template |
| Modify | `internal/vm/docker.go` — `-v` socket bind mounts |
| Modify | `internal/lifecycle/helpers.go` — populate bridges in start options |
| Modify | `internal/lifecycle/state.go` — include bridges in config hash |
| Modify | `aivm.example.yaml`, `README.md` |
| Test | `test/unit/config/` — validation, expansion |
| Test | `test/unit/vm/` — Lima template YAML generation |
| Test | `test/integration/` — Docker round-trip (host test socket → guest path) |

Not a plugin. Not in `plugins.enabled`.

## Testing

### Unit tests

- Config validation: duplicates, relative paths, env/`~` expansion
- `LimaPortForwardsYAML` renders `reverse: true` and absolute paths
- Docker volume flag generation for socket bridges (`:ro`)
- `ComputeConfigHash` changes when `socket_bridges` changes

### Integration tests (Docker backend)

1. Create a host Unix socket listener in test setup
2. Configure one bridge in test `aivm.yaml`
3. Start VM; inside container, `test -S guest_path` and connect (language-native
   or small Go helper — Alpine lacks `nc -U`)
4. Config change adds a bridge → triggers recreate / new bind

### Manual macOS (Lima)

- Bridge to a real host daemon socket
- Verify guest app connects via `guest_path`
- `aivm stop` / `aivm destroy` / recreate after config change
- Host daemon restart while VM running

## Security notes

- Socket bridges expose host services to VM processes — equivalent to granting
  VM code access to whatever API the host daemon exposes on that socket.
- Docker `:ro` on the bind mount prevents the container from replacing the socket
  inode via the mount point; it does not restrict protocol-level writes.
- Users should treat `socket_bridges` like privileged mounts — only bridge trusted
  host services.
