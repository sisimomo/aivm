# aivm Mount System Design

## Summary

Replace aivm's identity-only mount model with a unified, structured mount
system. Agent state mounts directly to the paths tools expect inside the VM
(no setup-script symlinks). User `vm.mounts` can remap host paths to different
guest paths. Host→guest path translation makes `aivm ssh` and agent launch work
correctly even when paths differ.

This is an alpha hard cut — no backward compatibility for the old string mount
format (`~/dev:rw`) or the old agent `persist` field.

This design **supersedes** the symlink-based approach in
`2026-06-12-claude-chat-history-persistence-design.md`. Claude history
persistence is achieved via agent `mounts` with explicit `mountPoint` values
instead of setup-script symlinks.

## Goals

- Fix agent state persistence so YAML-defined agents (Claude, Cursor, Copilot)
  write to the correct VM paths without per-agent symlink boilerplate
- Make `vm.mounts` more powerful: host `location` and guest `mountPoint` can
  differ
- Preserve identity mounts as a first-class case (`location == mountPoint`)
- Support `{{ .state_dir }}` and `{{ .home }}` templates in path fields (same
  engine as plugin setup scripts)
- Translate host CWD to guest path for `aivm ssh` and agent launch
- Hard cut from old config formats (alpha)

## Non-Goals

- Backward compatibility for string `vm.mounts` or agent `persist`
- File-level bind mounts (directories only)
- Path translation for `aivm cp` (VM paths stay explicit via `vm:` prefix)
- Agent-specific or user-defined template variables beyond `state_dir` and `home`
- Optional/default `mountPoint` (always required, fully explicit)
- Linux/Windows host support changes

## Background: current behavior

Today both `vm.mounts` and agent `persist` use **identity mounts**:

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
  MountSpec[]  (location, mountPoint, mode)
        │
        ▼
  Template render  ({{ .state_dir }}, {{ .home }})
        │
        ▼
  ~ expansion + validation
        │
        ▼
  ResolvedMount[]  (absolute host + guest paths)
        │
        ├─► VM create: Lima/Docker bind mounts (host → guest)
        ├─► AssertUnderMount: check host CWD under vm.mounts location
        └─► GuestPathForHost: translate CWD for ssh / agent launch
```

### Mount sources at VM create

`buildStartOptions` assembles mounts in order:

1. User `vm.mounts`
2. Enabled agent `mounts` (deduped by `mountPoint`)
3. Internal mounts (T3: `{{ .state_dir }}/.t3` → `{{ .home }}/.t3`)

All become `vm.Mount{HostPath, GuestPath, Writable}`.

## Config schema

### `MountSpec`

Used by `vm.mounts`, agent `mounts` (defaults and `agents.define`), and
internal mounts.

```yaml
location: string    # host-side bind source (required)
mountPoint: string  # guest-side path (required)
mode: rw | ro       # required
```

**Template support** (Go `text/template`, rendered on the host at config load):

| Variable | Value |
| --- | --- |
| `state_dir` | Resolved aivm state dir (`~/.aivm` or `AIVM_STATE_DIR`) |
| `home` | Host home directory |

After template render, `~` prefix expansion is applied to both path fields.
Both must be absolute paths before the mount is accepted.

### `vm.mounts` (user config)

```yaml
vm:
  mounts:
    # Identity mount — same path on host and guest
    - location: "{{ .home }}/dev"
      mountPoint: "{{ .home }}/dev"
      mode: rw
    # Remapped mount — host path differs from guest path
    - location: "{{ .home }}/company-secrets"
      mountPoint: "/secrets"
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
    - location: "{{ .state_dir }}/.claude/projects"
      mountPoint: "{{ .home }}/.claude/projects"
      mode: rw
    - location: "{{ .state_dir }}/.claude/image-cache"
      mountPoint: "{{ .home }}/.claude/image-cache"
      mode: rw

copilot:
  mounts:
    - location: "{{ .state_dir }}/.copilot/session-state"
      mountPoint: "{{ .home }}/.copilot/session-state"
      mode: rw

cursor:
  mounts:
    - location: "{{ .state_dir }}/.cursor"
      mountPoint: "{{ .home }}/.cursor"
      mode: rw
```

User override example:

```yaml
agents:
  define:
    claude:
      mounts:
        - location: "{{ .state_dir }}/.claude/projects"
          mountPoint: "{{ .home }}/.claude/projects"
          mode: rw
```

Agent `setup` scripts return to install-only. Remove symlink workarounds:

| Component | Remove |
| --- | --- |
| t3code plugin setup | `ln -sfn "$T3_DIR" "$HOME/.t3"` block |
| Claude setup (if added) | any symlink block |

T3 persistence becomes an internal `MountSpec` in `buildStartOptions`, same as
today but with explicit `mountPoint`.

## Runtime model

### `vm.Mount` (backend layer)

```go
type Mount struct {
    HostPath  string // rendered location
    GuestPath string // rendered mountPoint
    Writable  bool
}
```

**Lima** (at `limactl create`):

```
--mount type=bind,source=<HostPath>,target=<GuestPath>[,readonly]
```

Replaces the current `path:w` / `path:r` identity shorthand.

**Docker** (at `docker run`):

```
-v <HostPath>:<GuestPath>:<ro|rw>
```

Already supports distinct source/target; today both sides are the same path.

### Config load validation

| Rule | On violation |
| --- | --- |
| `location`, `mountPoint`, `mode` all required | Config load error |
| `mode` is `rw` or `ro` | Config load error |
| Paths absolute after render + `~` expand | Config load error |
| No duplicate `mountPoint` across all mounts | Config load error |
| No overlapping `location` prefixes among `vm.mounts` | Config load error |
| Template render failure | Config load error with field path |

Overlapping `location` prefixes among `vm.mounts` are rejected because they
make host→guest translation ambiguous. Agent `mounts` and internal mounts are
not checked for location overlap with `vm.mounts` (different purpose).

### Host directory creation

`ensureAgentPersistDirs` is renamed to reflect mounts (e.g.
`ensureAgentMountDirs`). It `MkdirAll`s each agent mount's rendered `location`
on the host before VM start. Only agent `mounts` — not user `vm.mounts`.

## Path translation

### `GuestPathForHost(hostPath, vmMounts) → (guestPath, error)`

Used by `aivm ssh` and agent launch (`aivm`, `aivm agent`).

Algorithm:

1. `hostPath` is already symlink-resolved (`resolveSessionCWD`)
2. Find the **longest matching** `location` prefix among **`vm.mounts` only**
3. `relative = hostPath[len(location):]` (empty string if exact match)
4. Return `mountPoint + relative`

**Examples:**

| Host CWD | Mount | Guest path |
| --- | --- | --- |
| `/Users/you/dev/myapp` | `location: /Users/you/dev` → `mountPoint: /Users/you/dev` | `/Users/you/dev/myapp` |
| `/Users/you/secrets/keys` | `location: /Users/you/secrets` → `mountPoint: /secrets` | `/secrets/keys` |

When `location == mountPoint`, translation is a no-op.

### Call sites

| Entry point | Change |
| --- | --- |
| `aivm ssh` | `VM.SSH(ctx, guestPath, env)` |
| `aivm` / `aivm agent` | `agentSession.vmDir = guestPath` |
| `AssertUnderMount` | **Unchanged** — validates host CWD under `vm.mounts` `location` |
| `aivm cp` | **Unchanged** — `vm:/path` is explicit guest path |

Agent `mounts` are for agent state directories, not workspace entry. Users
launch `aivm` from code under `vm.mounts`, not from `~/.aivm/`.

## Claude chat history (superseded spec)

Full-fidelity Claude history persistence is a **consumer** of this design:

| Data | location | mountPoint |
| --- | --- | --- |
| Session transcripts, subagents, tool-results | `{{ .state_dir }}/.claude/projects` | `{{ .home }}/.claude/projects` |
| Pasted images | `{{ .state_dir }}/.claude/image-cache` | `{{ .home }}/.claude/image-cache` |

Not mounted (ephemeral in VM): skills, MCP config, settings, `history.jsonl`,
`paste-cache/`.

Because project paths use sanitized absolute paths and identity `vm.mounts`
preserve host paths, resume works across VM recreations when launched from the
same project directory.

## Error handling

| Situation | Behavior |
| --- | --- |
| Old string mount in YAML | Parse/validate error at load |
| Old agent `persist` key in YAML | Rejected as unknown key (strict validation) |
| Template render fails | Config load error |
| Duplicate `mountPoint` | Config load error |
| Overlapping `vm.mounts` locations | Config load error |
| CWD not under any `vm.mounts` location | Existing `AssertUnderMount` error |
| `GuestPathForHost` no match after assert | Should not occur; treat as internal error |

## Code changes

| Area | Change |
| --- | --- |
| `internal/config/` | `MountSpec` struct, template renderer, validation; remove `ParseMount` string parser |
| `internal/config/config.go` | `VMConfig.Mounts` as `[]MountSpec`; update `AgentDefine` |
| `internal/agent/def.go` | Rename `Persist` → `Mounts []MountSpec` |
| `internal/agent/defaults.yaml` | Structured `mounts` for claude, copilot, cursor |
| `internal/vm/vm.go` | Add `GuestPath` to `Mount` |
| `internal/vm/lima.go` | Bind mount with explicit `source`/`target` |
| `internal/vm/docker.go` | `-v host:guest:mode` (distinct paths) |
| `internal/vm/ssh.go` | Update comment; `workDir` is guest path |
| `internal/lifecycle/helpers.go` | Mount assembly; rename persist dir helper |
| `internal/lifecycle/mountpath.go` | `GuestPathForHost`, validation helpers |
| `internal/lifecycle/agent_session.go` | Translate CWD before agent launch |
| `internal/lifecycle/commands.go` | Translate CWD before SSH |
| `internal/plugin/defaults.yaml` | Remove t3 symlink block |
| `aivm.example.yaml`, `README.md` | New mount format and agent `mounts` docs |
| Tests | Update all mount fixtures; translation and validation unit tests |

## Testing

### Unit

- Template rendering (`state_dir`, `home`, `~` expansion)
- Validation: duplicate `mountPoint`, overlapping locations, missing fields
- `GuestPathForHost`: identity, remapped, nested subdirs, longest-prefix match
- Lima/Docker mount flag generation with distinct `GuestPath`
- Agent defaults parse with `mounts` (not `persist`)

### E2E

- Remapped `vm.mounts` + `aivm ssh` lands in correct guest directory
- Agent launch from remapped mount uses correct guest `WorkDir`
- Claude history survives `aivm recreate` via agent `mounts` (no symlinks)
- `aivm cp vm:/guest/path` unchanged behavior

## Documentation updates

- `README.md` mounts section: structured format, template variables, remapped
  mount example, path translation note for ssh/agent
- Agent sections: `mounts` replaces `persist` language
- `aivm.example.yaml`: update `vm.mounts` example
- Note in `2026-06-12-claude-chat-history-persistence-design.md` header:
  superseded by this spec

## Migration (alpha hard cut)

Users must update `aivm.yaml`:

```yaml
# Before
vm:
  mounts:
    - "~/dev:rw"

# After
vm:
  mounts:
    - location: "{{ .home }}/dev"
      mountPoint: "{{ .home }}/dev"
      mode: rw
```

No migration tooling. Users run `aivm recreate` after upgrading to pick up new
agent mount paths baked into the VM.
