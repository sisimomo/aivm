# Base Image Design

## Summary

Add **base images** so aivm can recreate a VM or container from a post-bootstrap
snapshot instead of rerunning the full plugin bootstrap every time. Users who
recreate often (idle auto-delete, age prompts) get fast startup; a separate
**bootstrap refresh** timer still prompts for a full bootstrap periodically to
keep toolchains up to date.

Both supported backends implement the same lifecycle semantics:

| Backend | Save after bootstrap | Fast restore |
| --- | --- | --- |
| **Lima VZ** (Apple Silicon default) | Shadow instance clone (`<profile>-base`) | `limactl clone` from shadow |
| **Lima QEMU** | `limactl snapshot create --tag aivm-bootstrap` | `limactl snapshot apply` |
| **Docker** | `docker commit` → local tag | `docker run` from committed image |

Invalid or missing base images fall back to full bootstrap with a warning — never
block `aivm start`.

## Problem

Today, whenever the VM is destroyed (`idle.delete_timeout`, user-confirmed age
recreation, config-change recreation, `aivm recreate`), the next `aivm start`
creates a fresh instance and runs **full bootstrap** (all enabled plugins). That
can take minutes.

aivm already encourages regular recreation via:

- `vm.recreate_prompt_after` (default `7d`) — prompt to recreate an aged VM
- `idle.delete_timeout` — delete stopped VM after idle period

Without base images, those features trade hygiene for bootstrap latency.

## Goals

- Fast recreate from a saved post-bootstrap image when config hash is unchanged
- Full bootstrap on a configurable schedule (`vm.bootstrap_refresh_after`, default `30d`)
- Two independent timers: fast age recreation vs bootstrap refresh
- Invalidate base image on config hash change; keep existing “recreate to apply?” prompt (user can decline)
- `aivm recreate` → full bootstrap by default; `aivm recreate --fast` → restore from base
- Idle delete → start: silent fast restore when base is valid; prompt for full bootstrap when refresh is due (interactive only)
- Lima and Docker backends from day one
- Graceful degradation if save/restore fails

## Non-Goals

- Replacing Lima/Docker with Firecracker, Shuru, or a custom microVM runtime
- Direct Virtualization.framework bindings in Go (continue using Lima for the Lima backend)
- Base images that survive `vm.type` / backend switches (invalidation is sufficient)
- Publishing base images to a registry (local host only)
- Skipping bootstrap when config hash changes
- Linux/Windows host support

## Validation baseline

Local smoke test: `scripts/lima-clone-smoke-test.sh` on macOS (Lima VZ, Ubuntu
template, minimal `apt` bootstrap — jq + curl only).

| Metric | Measured | Notes |
| --- | --- | --- |
| Cold create + bootstrap | 40.35s | Simulated lightweight bootstrap |
| Save base (stop + clone) | 13.29s | Paid once per full bootstrap |
| Fast recreate | 15.98s | Clone + start + verify |
| Cold recreate | 46.52s | Create + bootstrap again |
| Fast vs cold speedup | **2.9×** | Expect **much higher** with real aivm plugins |
| Disk (live / shadow) | 3.37 GiB / 3.37 GiB | APFS copy-on-write right after save |

Real aivm bootstrap (mise, node, agents, optional docker) is far heavier than
the smoke test; the clone path avoids all of that work on recreate.

## Architecture

```text
Host state (~/.aivm/state/<profile>/)
├── bootstrap-state.json     # existing — skip bootstrap when hash matches
├── vm-created-at            # existing — vm.recreate_prompt_after
├── bootstrap-at             # NEW — vm.bootstrap_refresh_after
└── base-image.json          # NEW — snapshot metadata + validity

Lima (VZ)                         Docker
─────────                         ──────
aivm          (live)              aivm (container)
aivm-base     (stopped shadow)    aivm-base:<hash> (local image)

After full bootstrap: Save()       After full bootstrap: docker commit
Fast recreate: Restore()         Fast recreate: run from committed image
```

### `BaseImageStore` interface

New package or `internal/vm/baseimage` seam:

```go
type BaseImageStore interface {
    // Valid reports whether a restorable base exists for configHash.
    Valid(configHash string) bool
    // Save captures post-bootstrap state. Live VM may be restarted by implementation.
    Save(ctx context.Context, v VM, opts StartOptions) error
    // Restore creates/starts VM from base. Caller skips bootstrap when this succeeds.
    Restore(ctx context.Context, opts StartOptions) error
    // Invalidate marks base unusable (config change, manual recreate, migration).
    Invalidate() error
}
```

`LimaBaseImage` selects strategy from instance VM type:

- **QEMU** → `limactl snapshot` (tag `aivm-bootstrap`, experimental)
- **VZ** → shadow clone to `<profile>-base` (requires Lima ≥ 1.2, source stopped)

`DockerBaseImage` → `docker commit` / `docker run` from `aivm-<profile>-base` tag
(label with config hash; replace when hash changes).

### Host metadata: `base-image.json`

```json
{
  "version": "1",
  "config_hash": "<sha256>",
  "bootstrap_at": 1781150171,
  "backend": "lima",
  "strategy": "shadow-clone",
  "ref": "aivm-base"
}
```

`ref` meaning is backend-specific (shadow instance name, snapshot tag, or docker image ref).

## Configuration

| Key | Default | Purpose |
| --- | --- | --- |
| `vm.recreate_prompt_after` | `7d` | Prompt to **recreate VM** (fast path when base valid) |
| `vm.bootstrap_refresh_after` | `30d` | Prompt to **rerun full bootstrap** |

Both accept the same duration syntax as existing keys (`7d`, `12h`, `-1` to disable).

Excluded from `ComputeConfigHash` (like other prompt thresholds) — changing them
does not invalidate the base image.

Add to `internal/config/defaults.yaml` and `aivm.example.yaml`.

## Timestamps

| File | Set when | Used for |
| --- | --- | --- |
| `vm-created-at` | VM instance created or restored from base | `recreate_prompt_after` |
| `bootstrap-at` | Full bootstrap completes | `bootstrap_refresh_after` |

**Fast recreate:** reset `vm-created-at`; preserve `bootstrap-at` and `bootstrap-state.json`.

**Full bootstrap:** update all three; call `BaseImageStore.Save()`.

## Recreation modes

| Trigger | Default behavior |
| --- | --- |
| `aivm start`, VM missing, base valid | Fast restore; skip bootstrap |
| `aivm start`, VM missing, bootstrap refresh due + interactive | Prompt: full bootstrap or fast recreate |
| `aivm start`, VM missing, bootstrap refresh due + non-interactive | Fast restore (same as age prompt today) |
| `aivm start`, VM missing, no valid base | Full bootstrap + save base |
| Idle delete then start | Same as “VM missing” |
| Config hash changed | Invalidate base; prompt recreate (user can decline) |
| `aivm recreate` | Full bootstrap; refresh base |
| `aivm recreate --fast` | Restore from base if valid; error/warn if not |
| `syncBootstrap` stale/missing state | Existing recreate path; prefer full bootstrap + new base |

## Lifecycle: `Start` when VM not found

```text
VM StatusNotFound
    │
    ├─ base.Valid(configHash)?
    │     no ──► create VM ──► full bootstrap ──► Save base ──► done
    │
    yes
    │
    ├─ bootstrapRefreshDue() && interactive?
    │     yes ──► prompt: full bootstrap or fast recreate
    │              ├─ full ──► create VM ──► bootstrap ──► Save base
    │              └─ fast  ──► Restore base ──► apply env ──► done
    │
    └─ else ──► Restore base ──► apply vm.env + git identity ──► compose up ──► done
```

`ensureBootstrapped` gains a third path:

1. **Existing VM** → `syncBootstrap` (unchanged)
2. **Fresh VM, restored from base** → skip plugin bootstrap; re-apply `vm.env`, git identity, integrations if needed
3. **Fresh VM, no base** → full `bootstrap()` + `Save()`

Integrations: **skip** `runIntegrationsFromState` on fast restore. Configure scripts
are not guaranteed idempotent; full bootstrap and `aivm recreate` re-run them.
`bootstrap-state.json` remains valid — integrations are post-bootstrap wiring, not
captured in the base image metadata hash today.

## Lima: shadow clone (VZ)

Apple Silicon defaults to VZ. `limactl snapshot` is **not implemented** for VZ;
use a stopped shadow instance.

**Profile names:** live `vm.name` (default `aivm`), shadow `<profile>-base`.

**Save** (after successful full bootstrap):

1. Stop live instance (required — Lima rejects clone of running instances)
2. `limactl delete <profile>-base --force` if shadow exists
3. `limactl clone <profile> <profile>-base --start=false` (`limactl -y` in automation)
4. Start live instance
5. Write `base-image.json`

**Restore:**

1. Stop and delete live instance (container-equivalent destroy)
2. `limactl clone <profile>-base <profile> --start` with current `--cpus`, `--memory`,
   `--disk`, `--mount` flags from `StartOptions`
3. `WaitReady`; do not run plugin bootstrap

**Requirements:** Lima ≥ 1.2. Use `limactl -y` for non-interactive CI/scripts.

**Disk:** On APFS, clone uses copy-on-write; shadow and live share blocks until the
live VM diverges. Expect up to ~2× **actual** disk usage over time, not 2× sparse
allocation.

## Lima: snapshot (QEMU)

When `vm.type` is `qemu`:

- **Save:** `limactl stop`, `limactl snapshot create <profile> --tag aivm-bootstrap`
  (replace existing tag first)
- **Restore:** after recreate from template, `limactl snapshot apply --tag aivm-bootstrap`

Marked experimental in Lima; same graceful fallback on failure.

## Docker: commit

**Save:**

```text
docker commit <container> aivm-<profile>-base:latest
docker image label … config_hash=…
```

**Restore:**

- `docker run` using `aivm-<profile>-base:latest` instead of `vm.docker_image`
- Mounts and port mappings from `StartOptions` (same as today)

**Destroy:** `Destroy()` removes container only; base image retained until invalidated
or replaced. `destroy --all` / explicit invalidate removes base image.

## Invalidation

Call `BaseImageStore.Invalidate()` when:

- Config hash changes (before or when user confirms recreate)
- Full bootstrap completes (old base replaced by `Save()` — implicit invalidate + replace)
- `BootstrapVersion` migration in `bootstrap-state.json`
- Backend or `vm.type` changes
- `aivm recreate` without `--fast` (explicit full refresh)
- Save or restore fails (mark invalid; log warning)

Config hash change with user declining recreate: invalidate base but keep running VM
(user continues on stale VM without applying config — same as today).

## Prompts

### Bootstrap refresh (new)

When `bootstrap_refresh_after` is exceeded and `Confirmer.IsInteractive()`:

```text
WARN  Bootstrap is N day(s) old (threshold: 30 days)
  → Rerun full bootstrap to update toolchains? [y/N]
```

- **y** → full bootstrap path + new base
- **n** → fast recreate if base valid, or continue if VM exists

Non-interactive: skip prompt; use fast restore when base valid (consistent with
`checkVMAge` / `TestVMMaxAgeNonInteractiveSkipsPrompt`).

### VM age (`recreate_prompt_after`)

Unchanged wording. When user accepts recreation (`shouldRecreateVM` on stopped VM
or `checkVMAge` / `recreateCurrentVM`) and base is valid, use **fast restore**
instead of full bootstrap — unless bootstrap refresh is also due and the user
chose full bootstrap in that prompt.

## CLI

| Command | Behavior |
| --- | --- |
| `aivm recreate` | Destroy + full bootstrap + save base (today’s semantics, plus save) |
| `aivm recreate --fast` | Destroy + restore from base; skip bootstrap; fail with clear message if base invalid |

Optional: `aivm base-image status` — show valid/invalid, age, disk ref (nice-to-have, not required for v1).

## Error handling

- If `Save()` fails after successful bootstrap: log warning; VM remains usable; next recreate runs full bootstrap
- If `Restore()` fails: log warning; fall back to create + full bootstrap
- If shadow instance missing but metadata exists: treat as invalid
- Never leave user without a VM after `Start` if bootstrap can succeed

## Testing

| Layer | What |
| --- | --- |
| **Manual / macOS** | `scripts/lima-clone-smoke-test.sh` (clone timing, disk, constraints) |
| **Unit** | `base-image.json` read/write; invalidation; mode selection from timestamps |
| **E2E** | Idle delete → fast start skips bootstrap; bootstrap refresh prompt; `recreate --fast`; config change invalidates base |

Update e2e comments that mention “base image saved” to assert real behavior.

## Rejected alternatives

| Alternative | Reason |
| --- | --- |
| Firecracker on Mac | Requires KVM; nested VM adds complexity |
| Shuru / custom microVM runtime | Different product (ephemeral sandbox vs persistent dev runtime); large rewrite |
| QEMU as default on Apple Silicon | Much slower virtiofs/mounts; poor trade for daily agent use |
| `limactl snapshot` on VZ | Unimplemented (`errUnimplemented` in Lima VZ driver) |
| Pre-baked Dockerfile from plugin `setup` only | Misses configure steps, integrations, agent state |

## Open questions (resolved)

| Question | Decision |
| --- | --- |
| Two timers vs one? | Two: `recreate_prompt_after` + `bootstrap_refresh_after` |
| Config change? | Invalidate base; prompt; user can decline |
| Idle delete? | Silent fast restore; prompt if bootstrap refresh due (interactive) |
| `aivm recreate` default? | Full bootstrap; `--fast` for base restore |
| Backends? | Lima + Docker day one |
