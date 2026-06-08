# Remove skip_if Design

## Summary

Remove `skip_if` from plugins, agents, and integrations. Bootstrap always
runs `setup` and `configure` scripts on a fresh VM. Drop the reconcile path
(`force=false` + per-plugin skip guards), the unused in-VM bootstrap marker,
and the dead `LifecycleService.Bootstrap` method. Stale host bootstrap state
triggers VM recreation instead of in-place reconcile.

aivm is alpha; this is a breaking change with no migration warnings.

## Background

`skip_if` is a shell script run before `setup` (plugins/agents) or
`configure` (integrations). Exit code 0 means "already done — skip."

It was designed for **re-bootstrap on a persistent VM**: skip redundant
`apt-get`, curl installers, and MCP registration when the VM disk survives
but bootstrap runs again.

The intended product model is **ephemeral VMs**: destroy and recreate on config
change. In that model:

- Fresh VM bootstrap uses `force=true`, which bypasses `skip_if` entirely.
- Unchanged config on an existing VM skips bootstrap via host
  `bootstrap-state.json` hash — `skip_if` never runs.
- Config change prompts recreate → fresh VM → `force=true` again.

`skip_if` therefore adds complexity without meaningful runtime value for the
ephemeral model. The only live use during fresh bootstrap is retry tolerance
when a setup script errors but partially succeeded — narrow and replaceable
with setup retries alone.

## Goals

- Remove `skip_if` from the plugin, agent, and integration YAML schemas and Go
  types.
- Remove the `force` parameter from bootstrap execution (always run all
  setups).
- Remove the in-VM `~/.aivm-bootstrap-version` marker file (written but never
  read).
- Delete unused `LifecycleService.Bootstrap()` method (not wired to CLI).
- Route stale/missing host bootstrap state to `recreateVM()` instead of
  `fullBootstrap(force=false)`.
- Bump host `BootstrapVersion` to `"3"` so existing installs with v2 state
  recreate on next start.
- Document new contract: `setup`/`configure` scripts must be safe on a fresh VM.

## Non-Goals

- **Always recreate on config change** — keep the existing prompt ("recreate or
  continue without applying"). Only stale-state handling changes.
- **Remove host `bootstrap-state.json`** — still used to skip bootstrap when
  config is unchanged (`syncBootstrap` up-to-date check).
- **Remove `vm.env` in-place updates** — `envChangedStep` unchanged.
- **Make setup scripts smarter** — no new `verify` field; authors use
  idempotent installers (`apt-get install -y`, `mise use`, etc.).
- **Ship built-in integrations** — `defaults.yaml` stays `[]`; integration
  schema just loses `skip_if`.

## Bootstrap Model (After)

```text
┌─────────────────────────────────────────────────────────────┐
│  aivm start                                                 │
├─────────────────────────────────────────────────────────────┤
│  VM not found?  → create VM → bootstrap() [all setups]      │
│  VM exists?     → syncBootstrap():                          │
│    • state missing/stale  → recreateVM()                    │
│    • config changed       → prompt recreate (unchanged)     │
│    • env changed only     → apply vm.env in place           │
│    • up to date           → skip bootstrap entirely         │
└─────────────────────────────────────────────────────────────┘
```

Within `bootstrap()`:

1. Resolve enabled plugins + agents into a DAG.
2. Write `/etc/profile.d/aivm-path.sh`.
3. Run each plugin's `setup` in order (up to 3 retries on failure).
4. Apply `vm.env`, git identity, record host bootstrap state.
5. Run matching integrations' `configure` scripts (no skip guard).

## Schema Changes

### Plugins and agents (before → after)

**Before:**

```yaml
my-tool:
  description: "My tool"
  dependencies: [system]
  skip_if: |
    command -v my-tool >/dev/null 2>&1
  setup: |
    curl -fsSL https://example.com/install.sh | bash
```

**After:**

```yaml
my-tool:
  description: "My tool"
  dependencies: [system]
  setup: |
    curl -fsSL https://example.com/install.sh | bash
```

Same for all entries in `internal/agent/defaults.yaml` and
`internal/plugin/defaults.yaml`.

### Integrations (before → after)

**Before:**

```yaml
integrations:
  - from: my-tool
    to: claude
    skip_if: |
      my-tool status --agent claude >/dev/null 2>&1
    configure: |
      my-tool configure --agent claude
```

**After:**

```yaml
integrations:
  - from: my-tool
    to: claude
    configure: |
      my-tool configure --agent claude
```

### Go API (before → after)

| Before | After |
| --- | --- |
| `Plugin.SkipIf(ctx, env) (bool, error)` | removed |
| `Executor.Run(ctx, force bool)` | `Executor.Run(ctx)` |
| `Engine.Run(ctx, force bool)` | `Engine.Run(ctx)` |
| `fullBootstrap(ctx, vm, force bool)` | `bootstrap(ctx, vm)` |
| `LifecycleService.Bootstrap(...)` | deleted |
| `IntegrationDef.SkipIf` | removed |

## Behaviour Changes

### Fresh VM (unchanged outcome, simpler path)

`ensureBootstrapped` calls `bootstrap()` which runs every `setup`. No
`skip_if`, no `force` flag.

### Second start, unchanged config (unchanged)

`syncBootstrap` → `upToDateStep` → "VM is up to date — skipping bootstrap".
No scripts run.

### Stale bootstrap state (changed)

**Before:** `missingOrStaleStep` → `fullBootstrap(force=false)` →
"Reconciling VM bootstrap" → `skip_if` gates each plugin.

**After:** `missingOrStaleStep` → `recreateVM()` → destroy + fresh
`bootstrap()`.

### Setup retry (changed)

**Before:** On retry or final error, re-check `skip_if` to detect partial
install.

**After:** Retry `setup` up to 3 times; fail if still erroring. No
post-error skip guard.

### Integrations (changed when used)

**Before:** Check `skip_if` before `configure`.

**After:** Always run `configure` when `from`/`to` match. On ephemeral VMs
this runs once per bootstrap.

## Removed Artifacts

| Artifact | Reason |
| --- | --- |
| `~/.aivm-bootstrap-version` in VM | Written by bootstrap engine, never read |
| `markerFile` constant | Only used for above |
| `LifecycleService.Bootstrap()` | Dead code — not exposed in CLI |
| `AssertSkipIf` test helper | Tests install outcome via `AssertCommand` instead |
| `AssertIntegrationConfigured` | Depended on integration `skip_if`; no callers |
| Mise `skipIfScript()` | Mise `setup` is already idempotent |

## Host Bootstrap Version

`bootstrap.BootstrapVersion` remains in `bootstrap-state.json` on the host
(not in the VM). Bump from `"2"` to `"3"` for this change so installations
with outdated state recreate on next `aivm start`.

## Author Contract

Plugin and integration authors must write **idempotent `setup`/`configure`
scripts** safe to run on a blank VM:

- `apt-get install -y` — safe to re-run
- `mise use --global` — idempotent
- `npm install -g` — acceptable on fresh VM
- Installer scripts that no-op when already present — fine, but aivm will not
  detect that; it always invokes `setup`

The `skills` plugin already follows this pattern (no `skip_if`, always runs
`npx skills@latest add`).

## Testing

### Unit tests

- Remove `skip_if` presence checks from agent/plugin defaults tests.
- Remove all `TestMisePlugin_SkipIf_*` tests; keep `Setup` tests.
- Delete `TestSkillsPlugin_NoSkipIf` (no longer special).
- Integration defaults test: validate `configure` templates only.

### Bootstrap tests (Docker)

- Remove all `AssertSkipIf` calls; keep `AssertCommand` verifying binaries
  and config files after install.
- Harness `Install()` calls `Executor.Run(ctx)` without `force`.

### E2E tests

- Rewrite `TestStartRerunBootstrapAfterVersionMismatch` → expect VM recreate
  and "bootstrap state missing or outdated" warning, not "Reconciling VM
  bootstrap".
- `TestStartSkipsBootstrapWhenUpToDate` — unchanged.

## Documentation

- Remove all `skip_if` references from `README.md` and `aivm.example.yaml`.
- Add one line: setup scripts must be idempotent on a fresh VM.
- Update plugin schema tables (remove `skip_if` row).

## Implementation

See `docs/superpowers/plans/2026-06-08-remove-skip-if.md`.
