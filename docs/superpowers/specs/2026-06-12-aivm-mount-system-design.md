# aivm Mount System Design

## Summary

Replace aivm's same-path-only mount model with a unified, structured mount
system. Agent state mounts directly to the paths tools expect inside the VM
(no setup-script symlinks). User `vm.mounts` can remap host paths to different
guest paths. Host→guest path translation makes `aivm ssh` and agent launch work
correctly even when paths differ.

This is an alpha hard cut — no backward compatibility for the old string mount
format (`~/dev:rw`) or the old agent `persist` field.

## Goals

- Fix agent state persistence so YAML-defined agents (Claude, Cursor, Copilot)
  write to the correct VM paths without per-agent symlink boilerplate
- Make `vm.mounts` more powerful: host `source` and guest `target` can differ
- Preserve same-path mounts as a first-class case (`source` and `target` resolve
  to the same logical path; on Lima/macOS use `{{ .host_home }}` in `target` so
  the VM sees the same absolute path as on the host)
- Support `{{ .state_dir }}` and `{{ .host_home }}` templates in path fields
  (same engine as plugin setup scripts)
- Expand `~` in `source` to host home and in `target` to VM user home
- Translate host CWD to guest path for `aivm ssh` and agent launch
- Hard cut from old config formats (alpha)

## Non-Goals

- Backward compatibility for string `vm.mounts` or agent `persist`
- File-level bind mounts (directories only)
- Path translation for `aivm cp` (VM paths stay explicit via `vm:` prefix)
- Agent-specific or user-defined template variables beyond `state_dir` and
  `host_home`
- User-configurable VM home override (`vm.guest_home` is rejected at load)
- Optional/default `target` (always required, fully explicit)
- Linux/Windows host support changes

## Background: current behavior

Today both `vm.mounts` and agent `persist` use **same-path mounts**:

| Config | Parsed as | VM bind |
| --- | --- | --- |
| `vm.mounts: ["~/dev:rw"]` | `HostPath = /Users/you/dev` | `/Users/you/dev` → `/Users/you/dev` |
| `persist: [.claude/projects]` | `HostPath = ~/.aivm/.claude/projects` | same path in VM |

Agent `persist` data is stored under `~/.aivm/` on the host but mounted at
that same path in the VM — **not** where agents expect it (`~/.claude/projects`,
etc.). The t3code plugin works around this with `ln -sfn` in its setup script.
Claude has no equivalent wiring.

`aivm ssh` and agent launch pass the host CWD directly to the VM (`cd
<hostPath>`), which only works when host and guest paths are identical.

## Architecture

```text
aivm.yaml / agent defaults
        │
        ▼
  MountSpec[]  (source, target, mode)
        │
        ▼
  Template render  ({{ .state_dir }}, {{ .host_home }})
        │
        ▼
  ~ expansion (host home in source, VM home in target) + validation
        │
        ▼
  ResolvedMount[]  (absolute host + guest paths)
        │
        ├─► VM create: Lima/Docker bind mounts (host → guest)
        ├─► AssertUnderMount: check host CWD under vm.mounts source
        └─► GuestPathForHost: translate CWD for ssh / agent launch
```

### Mount sources at VM create

`buildStartOptions` assembles mounts in order:

1. User `vm.mounts`
2. Enabled agent `mounts` (deduped by `target`)
3. Internal mounts (T3: `{{ .state_dir }}/.t3` → `~/.t3`)

All become `vm.Mount{HostPath, GuestPath, Writable}`.

## Config schema

### `MountSpec`

Used by `vm.mounts`, agent `mounts` (defaults and `agents.define`), and
internal mounts.

```yaml
source: string   # host-side bind source (required)
target: string   # guest-side path (required)
mode: rw | ro    # required
```

**Template support** (Go `text/template`, rendered on the host at config load):

| Variable | Value |
| --- | --- |
| `state_dir` | Resolved aivm state dir (`~/.aivm` or `AIVM_STATE_DIR`) |
| `host_home` | Host home directory |

Use `~` in `target` for VM-user-home-relative paths (agent state mounts).
There is no `{{ .guest_home }}` template — VM home is not a template variable.

After template render, `~` prefix expansion is applied:

- In `source`: `~` → host home
- In `target`: `~` → VM user home (derived from backend; see below)

Both must be absolute paths before the mount is accepted.

### VM user home (derived, not configurable)

VM user home is determined automatically from the VM backend and stored as
`VMConfig.ParsedVMHome` at config load. It is used when expanding `~` in mount
`target` paths. Users cannot override it — `vm.guest_home` in YAML is rejected
with a clear error.

| Backend | Default VM home |
| --- | --- |
| `docker` | `/home/user` |
| `lima` on Linux | `/home/$USER` |
| `lima` on macOS | `/home/$USER.guest` |

Implemented in `mountspec.DefaultVMHome()`.

### `vm.mounts` (user config)

```yaml
vm:
  mounts:
    # Same-path mount (recommended on Lima/macOS) — VM sees host absolute path
    - source: "~/dev"
      target: "{{ .host_home }}/dev"
      mode: rw
    # Parallel mount — both sides use ~; VM path lands under VM $HOME
    # (e.g. /home/you.guest/dev on Lima macOS)
    - source: "~/scratch"
      target: "~/scratch"
      mode: rw
    # Remapped mount — host path differs from guest path
    - source: "{{ .host_home }}/company-secrets"
      target: "/secrets"
      mode: ro
```

The old string form (`~/dev:rw`) is **removed**. Config load fails with a
clear error if a mount entry is not a structured object.

### Agent `mounts` (defaults + `agents.define.<agent>.mounts`)

Rename `persist` → `mounts` everywhere:

- `internal/agent/defaults.yaml` bundled defaults
- `agent.Def.Mounts` struct field
- `agents.define.<name>.mounts` user overrides in `aivm.yaml`

```yaml
# internal/agent/defaults.yaml (bundled)
claude:
  mounts:
    - source: "{{ .state_dir }}/.claude/projects"
      target: "~/.claude/projects"
      mode: rw
    - source: "{{ .state_dir }}/.claude/image-cache"
      target: "~/.claude/image-cache"
      mode: rw

copilot:
  mounts:
    - source: "{{ .state_dir }}/.copilot/session-state"
      target: "~/.copilot/session-state"
      mode: rw

cursor:
  mounts:
    - source: "{{ .state_dir }}/.cursor"
      target: "~/.cursor"
      mode: rw
```

User override example:

```yaml
agents:
  define:
    claude:
      mounts:
        - source: "{{ .state_dir }}/.claude/projects"
          target: "~/.claude/projects"
          mode: rw
```

Agent `setup` scripts return to install-only. Remove symlink workarounds:

| Component | Remove |
| --- | --- |
| t3code plugin setup | `ln -sfn "$T3_DIR" "$HOME/.t3"` block |

T3 persistence becomes an internal `MountSpec` in `ResolvedMountsForStart`:

```yaml
source: "{{ .state_dir }}/.t3"
target: "~/.t3"
```

## Runtime model

### `vm.Mount` (backend layer)

```go
type Mount struct {
    HostPath  string // rendered source
    GuestPath string // rendered target
    Writable  bool
}
```

**Lima** (at `limactl create`):

Remapped mounts are written into the Lima instance template YAML (not
`limactl --mount`, which only supports same guest path as host):

```yaml
mounts:
  - location: "<HostPath>"
    mountPoint: "<GuestPath>"   # omitted when HostPath == GuestPath
    writable: true
```

**Docker** (at `docker run`):

```text
-v <HostPath>:<GuestPath>:<ro|rw>
```

### Config load validation

| Rule | On violation |
| --- | --- |
| `source`, `target`, `mode` all required | Config load error |
| `mode` is `rw` or `ro` | Config load error |
| Paths absolute after render + `~` expand | Config load error |
| No duplicate `target` among `vm.mounts` | Config load error |
| No overlapping `source` prefixes among `vm.mounts` | Config load error |
| Template render failure | Config load error with field path |
| `vm.guest_home` key present | Config load error (not supported) |

Overlapping `source` prefixes among `vm.mounts` are rejected because they
make host→guest translation ambiguous. Agent `mounts` and internal mounts are
not checked for source overlap with `vm.mounts` (different purpose).

At VM start, `ResolvedMountsForStart` validates duplicate `target` across
user, agent, and internal mounts (agent duplicates are skipped with a trace
log during assembly).

### Host directory creation

`ensureAgentPersistDirs` is renamed to `ensureAgentMountDirs`. It `MkdirAll`s
each agent mount's rendered `source` on the host before VM start, and the T3
mount source when T3 is enabled. User `vm.mounts` sources are not auto-created.

## Path translation

### `GuestPathForHost(hostPath, cfg) → (guestPath, error)`

Used by `aivm ssh` and agent launch (`aivm`, `aivm agent`).

Algorithm:

1. `hostPath` is already symlink-resolved (`resolveSessionCWD`)
2. Find the **longest matching** `source` prefix among **`vm.mounts` only**
3. `relative = hostPath[len(source):]` (empty string if exact match)
4. Return `target + relative`

**Examples:**

| Host CWD | Mount | Guest path |
| --- | --- | --- |
| `/Users/you/dev/myapp` | `source: /Users/you/dev` → `target: /Users/you/dev` | `/Users/you/dev/myapp` |
| `/Users/you/dev/myapp` | `source: /Users/you/dev` → `target: /home/you.guest/dev` | `/home/you.guest/dev/myapp` |
| `/Users/you/secrets/keys` | `source: /Users/you/secrets` → `target: /secrets` | `/secrets/keys` |

When resolved `source` and `target` use a same-path mount (e.g.
`{{ .host_home }}/dev` on both sides on macOS), translation preserves the
relative suffix.

### Call sites

| Entry point | Change |
| --- | --- |
| `aivm ssh` | `VM.SSH(ctx, guestPath, env)` |
| `aivm` / `aivm agent` | `agentSession.vmDir = guestPath` |
| `AssertUnderMount` | **Unchanged** — validates host CWD under `vm.mounts` `source` |
| `aivm cp` | **Unchanged** — `vm:/path` is explicit guest path |

Agent `mounts` are for agent state directories, not workspace entry. Users
launch `aivm` from code under `vm.mounts`, not from `~/.aivm/`.

## Claude agent mounts

Claude Code conversation history must survive `aivm destroy` and `aivm recreate`
with full fidelity (main transcripts, subagents, large tool outputs, pasted
images). Skills, MCP config, and other Claude settings stay ephemeral in the VM.

Bundled defaults in `internal/agent/defaults.yaml`:

```yaml
claude:
  mounts:
    - source: "{{ .state_dir }}/.claude/projects"
      target: "~/.claude/projects"
      mode: rw
    - source: "{{ .state_dir }}/.claude/image-cache"
      target: "~/.claude/image-cache"
      mode: rw
```

| Mount | Covers |
| --- | --- |
| `projects` | Session transcripts, `subagents/`, `tool-results/` |
| `image-cache` | Pasted images per session |

Host data lives under `~/.aivm/.claude/` and survives VM lifecycle. Guest
paths use `~` so data appears at `$HOME/.claude/…` inside the VM. Not mounted
(ephemeral): `settings.json`, skills, MCP config, `history.jsonl`, `paste-cache/`.

Because Claude groups sessions by sanitized absolute project path and same-path
`vm.mounts` preserve host paths, resume works across VM recreations when
launched from the same project directory.

## Error handling

| Situation | Behavior |
| --- | --- |
| Old string mount in YAML | Parse/validate error at load |
| Old agent `persist` key in YAML | Rejected as unknown key (strict validation) |
| `vm.guest_home` in YAML | Config load error (not supported) |
| Template render fails | Config load error |
| Duplicate `target` among `vm.mounts` | Config load error |
| Overlapping `vm.mounts` sources | Config load error |
| CWD not under any `vm.mounts` source | Existing `AssertUnderMount` error |
| `GuestPathForHost` no match after assert | Should not occur; treat as internal error |

## Code changes

| Area | Change |
| --- | --- |
| `internal/mountspec/` | `MountSpec`, template renderer, contextual `~` expansion, `DefaultVMHome`, validation |
| `internal/config/config.go` | `VMConfig.Mounts` as `[]MountSpec`; `ParsedVMHome`; update `AgentDefine` |
| `internal/config/parse.go` | Reject `vm.guest_home`; remove `ParseMount` string parser |
| `internal/agent/def.go` | Rename `Persist` → `Mounts []MountSpec` |
| `internal/agent/defaults.yaml` | Structured `mounts` for claude, copilot, cursor |
| `internal/vm/vm.go` | Add `GuestPath` to `Mount` |
| `internal/vm/lima.go` | Lima template YAML mounts with `location` / `mountPoint` |
| `internal/vm/mountflag.go` | `LimaMountYAML`, `DockerVolumeFlag` helpers |
| `internal/vm/docker.go` | `-v host:guest:mode` (distinct paths) |
| `internal/vm/ssh.go` | Update comment; `workDir` is guest path |
| `internal/lifecycle/helpers.go` | `ResolvedMountsForStart`; `ensureAgentMountDirs` |
| `internal/lifecycle/mountpath.go` | `GuestPathForHost` |
| `internal/lifecycle/agent_session.go` | Translate CWD before agent launch |
| `internal/lifecycle/commands.go` | Translate CWD before SSH |
| `internal/plugin/defaults.yaml` | Remove t3 symlink block |
| `aivm.example.yaml`, `README.md` | New mount format and agent `mounts` docs |
| Tests | Update all mount fixtures; translation and validation unit tests |

## Testing

### Unit

- Template rendering (`state_dir`, `host_home`, contextual `~`)
- Validation: duplicate `target`, overlapping sources, missing fields
- `GuestPathForHost`: same-path, remapped, nested subdirs, longest-prefix match
- Lima/Docker mount flag generation with distinct `GuestPath`
- Agent defaults parse with `mounts` (not `persist`)
- Reject `vm.guest_home` at config load

### E2E

- Remapped `vm.mounts` + `aivm ssh` lands in correct guest directory
- Agent launch from remapped mount uses correct guest `WorkDir`
- Claude history survives `aivm recreate` via agent `mounts` (no symlinks)
- `aivm cp vm:/guest/path` unchanged behavior

## Documentation updates

- `README.md` mounts section: `source`/`target`, contextual `~`, derived VM home,
  `{{ .host_home }}` template, remapped mount example, path translation note for
  ssh/agent
- Agent sections: `mounts` replaces `persist` language; Claude persistence
  scope (`projects`, `image-cache` on host under `~/.aivm/.claude/`)
- `aivm.example.yaml`: update `vm.mounts` example

## Migration (alpha hard cut)

Users must update `aivm.yaml`:

```yaml
# Before
vm:
  mounts:
    - "~/dev:rw"

# After (recommended on Lima/macOS — same absolute path in VM)
vm:
  mounts:
    - source: "~/dev"
      target: "{{ .host_home }}/dev"
      mode: rw
```

No migration tooling. Users run `aivm recreate` after upgrading to pick up new
agent mount paths baked into the VM.
