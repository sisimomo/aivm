# Base Image Design

## Summary

After a successful bootstrap, aivm saves a **base image** of the VM or container.
The next time the runtime is recreated (idle delete, age prompt, manual recreate),
aivm can **restore from that image** instead of rerunning the full bootstrap.

This keeps recreation fast while a separate **bootstrap refresh** timer still
prompts users to rerun bootstrap periodically so toolchains stay up to date.

Base images are **enabled by default**, can be turned off in config, and must
never block normal operation: if no base exists, restore fails, or save fails,
aivm falls back to today’s behavior (create + full bootstrap).

## Problem

Whenever the VM is destroyed and recreated, aivm runs the full bootstrap again.
That is slow — especially painful when combined with:

- `idle.delete_timeout` — auto-delete after idle
- `vm.recreate_prompt_after` (default `7d`) — encourage regular recreation

Users are pushed to recreate often, but pay the full bootstrap cost every time.

## What we want

- **Fast recreate** when nothing meaningful changed — restore post-bootstrap state
- **Full bootstrap** on a schedule (default every 30 days) to refresh packages and plugins
- **Two independent timers** — VM age vs bootstrap age
- **Config opt-out** — feature on by default, disable if unwanted
- **Safe fallback** — enabled with no base, or a broken base, must work like today
- **No hangs** — a bad base is deleted and aivm continues with full bootstrap
- **Lima and Docker** both supported

## Configuration

| Key | Default | Purpose |
| --- | --- | --- |
| `vm.base_image_enable` | `true` | Use base images for fast recreate |
| `vm.recreate_prompt_after` | `7d` | Prompt to recreate the VM (fast when base is valid) |
| `vm.bootstrap_refresh_prompt_after` | `30d` | Prompt to rerun full bootstrap |

Duration syntax matches existing aivm keys (`7d`, `12h`, `-1` to disable).

Changing these keys does **not** invalidate the base image.

### When base images are disabled (`vm.base_image_enable: false`)

- No save after bootstrap
- Every recreate runs full bootstrap (same as today)
- The `--fast` flag has no effect — every recreate runs full bootstrap

### When base images are enabled (default)

- After full bootstrap, save a base image (best effort)
- On recreate, restore from base when valid — skip bootstrap
- If **no base exists yet**, behave exactly like today: create + full bootstrap, then try to save
- If **restore or save fails**, delete the broken base, warn the user, and continue with full bootstrap — do not hang or leave aivm in a bad state

## Host state

Host state lives under `~/.aivm/state/<profile>/` (same as today).

| File | Set when | Used for |
| --- | --- | --- |
| `vm-created-at` | VM instance created or restored from base | `recreate_prompt_after` |
| `bootstrap-at` | Full bootstrap completes | `bootstrap_refresh_prompt_after` |
| `bootstrap-state.json` | Full bootstrap completes | Skip plugin bootstrap when hash matches; base validity check |

`bootstrap-state.json` gains a `backend` field (`lima` or `docker`) recorded at
full bootstrap. It is not part of `config_hash` but is checked separately at
restore time.

**Missing `bootstrap-at`:** bootstrap age is not due — skip bootstrap refresh
prompts; run full bootstrap normally on first create.

**Fast recreate:** reset `vm-created-at`; preserve `bootstrap-at` and `bootstrap-state.json`.

**Full bootstrap:** update all three; replace the base image via save.

There is no separate `base-image.json`. Base validity is determined at restore
time by checking:

1. The backend artifact exists (Lima shadow instance or Docker committed image), and
2. `bootstrap-state.json` `config_hash` matches the current config hash, and
3. `bootstrap-state.json` `backend` matches the current `vm.backend`.

If any check fails, treat the base as invalid: delete the artifact and fall back
to full bootstrap.

## Two clocks

aivm tracks two separate ages:

| Clock | Tracks | Used for |
| --- | --- | --- |
| **VM age** | When the current VM instance was created (`vm-created-at`) | `recreate_prompt_after` |
| **Bootstrap age** | When bootstrap last ran fully (`bootstrap-at`) | `bootstrap_refresh_prompt_after` |

**Fast recreate** resets VM age but keeps bootstrap age.

**Full bootstrap** resets both and refreshes the base image.

## Prompt entry points

VM age and bootstrap refresh are evaluated at the **same entry points**:

| Entry point | When |
| --- | --- |
| `aivm start` | VM is stopped or missing |
| `aivm agent` / launch | VM is running |

## When to use which path

| Situation | Behavior |
| --- | --- |
| Base images disabled | Always full bootstrap |
| VM missing, no base saved yet | Full bootstrap; try to save base afterward |
| VM missing, valid base, no prompt due | Fast restore; skip bootstrap (silent) |
| Idle delete, then `aivm start`, only VM age due (interactive) | Silent fast restore when base valid |
| Idle delete, then `aivm start`, bootstrap refresh due (interactive) | Bootstrap refresh or combined prompt (see Prompts) |
| Bootstrap refresh due only + interactive | Bootstrap refresh prompt |
| VM age due only + interactive + VM exists | VM age prompt as today; fast restore when base valid |
| Both timers due + interactive + VM exists | Combined prompt (3 options; see Prompts) |
| Both timers due + interactive + VM missing | Combined prompt (2 options: full bootstrap vs fast restore) |
| Timer due + non-interactive + VM exists | Resume VM as today — no prompt, no recreation |
| Timer due + non-interactive + VM missing | Fast restore when base valid; else full bootstrap |
| Config hash changed since last bootstrap | Delete base; prompt to recreate (user can decline, as today) |
| `vm.backend` changed | Prompt; accept = full wipe like `aivm destroy` (see Backend change) |
| `BootstrapVersion` stale in bootstrap state | Delete base; full bootstrap on next create |
| Restore fails | Delete base; full bootstrap |
| `aivm recreate` | Full bootstrap; refresh base |
| `aivm recreate --fast` | Fast restore if base valid; otherwise full bootstrap (warn once) |
| `aivm destroy` | Remove live VM, base image, and all host bootstrap/age state |
| `aivm destroy --keep-base` | Remove live VM only; retain base and host state for fast recreate |

After fast restore, aivm still reapplies `vm.env` and git identity. Integrations
(compose configure scripts) run only on full bootstrap — not after fast restore.
`syncBootstrap` sees matching `bootstrap-state.json` and skips plugin bootstrap.

## Prompts

When only one timer is due, use the single-timer prompts below. When **both**
timers are due, show one combined prompt — never chain two separate prompts.

### Bootstrap refresh only

When bootstrap is older than `bootstrap_refresh_prompt_after`, VM age is within
threshold, and the terminal is interactive:

```text
WARN  Bootstrap is N day(s) old (threshold: 30 days)
  → Rerun full bootstrap to update toolchains? [y/N]
```

- **y** — full bootstrap and new base image
- **n** — if VM is **running**, keep it as-is (no recreation); if VM is **stopped or missing**, fast restore from base when valid

### VM age only (`recreate_prompt_after`)

Same prompt as today. When the user accepts recreation and a valid base exists,
use fast restore.

### Both timers due (combined)

When both thresholds are exceeded and the terminal is interactive:

**VM exists** — three options:

```text
WARN  Bootstrap is N day(s) old (threshold: 30 days)
WARN  VM is M day(s) old (threshold: 7 days)
  → [1] Rerun full bootstrap to update toolchains
  → [2] Fast recreate from base image
  → [3] Continue without changes
```

- **1** — full bootstrap, new base, both clocks reset
- **2** — fast restore if base valid; otherwise full bootstrap (warn once); stops active sessions if needed
- **3** — keep current VM as-is; no recreation

**VM missing** (e.g. after idle delete) — two options only (no “continue”):

```text
WARN  Bootstrap is N day(s) old (threshold: 30 days)
WARN  VM is M day(s) old (threshold: 7 days)
  → [1] Rerun full bootstrap to update toolchains
  → [2] Fast recreate from base image
```

### Non-interactive

Skip all prompts.

- **VM exists** (stopped or running): resume as today — no recreation
- **VM missing**: fast restore when base is valid; otherwise full bootstrap

## Backend change

Changing `vm.backend` (e.g. `lima` → `docker`) is equivalent to `aivm destroy`
followed by a fresh start on the new backend. aivm prompts before proceeding:

```text
WARN  VM backend has changed (lima → docker)
  → This will destroy the VM and base image. Continue? [y/N]
```

- **y** — remove live VM, base image, and all host bootstrap/age state; next
  `aivm start` creates and bootstraps on the new backend
- **n** — keep the current setup as-is; warn that the config change was not
  applied (same as declining a config-change recreation today)

## CLI

| Command | Behavior |
| --- | --- |
| `aivm destroy` | Destroy live VM/container, delete base image, clear `bootstrap-state.json`, `bootstrap-at`, and `vm-created-at` |
| `aivm destroy --keep-base` | Destroy live VM/container only; keep base artifact and all host state files |
| `aivm recreate` | Destroy, full bootstrap, save new base |
| `aivm recreate --fast` | Restore from base if possible; otherwise full bootstrap |

`--fast` on `recreate` has no effect when `vm.base_image_enable: false`.

Default `destroy` is a stronger wipe than today (also removes base image and
bootstrap state). Use `--keep-base` to preserve the base for fast recreate.

## How base images are stored

### Lima — shadow clone

A stopped **shadow Lima instance** holds the post-bootstrap disk state. Shadow
clone is used for both `vm.type: vz` and `vm.type: qemu` (Lima ≥ 1.2).

- Live instance: `vm.name` (default `aivm`)
- Shadow instance: `<name>-base` (e.g. `aivm-base`)

**Save** (after successful full bootstrap):

1. Stop the live instance (Lima cannot clone a running instance)
2. Delete any existing shadow instance
3. Clone live → shadow (`limactl clone`), leave shadow stopped
4. Start the live instance again

**Fast restore:**

1. Delete the live instance
2. Clone shadow → live with current CPU, memory, disk, and mounts from config
3. Start the live instance — skip bootstrap

Use `limactl -y` for non-interactive automation.

**Disk:** On APFS, clone shares disk blocks until the live VM changes; expect disk
use to grow toward roughly twice the actual VM footprint over time, not twice
the configured disk size.

### Docker — commit

**Save** (after successful full bootstrap):

- Commit the running container to a local image tagged for this profile
  (e.g. `aivm-<profile>-base`)

**Fast restore:**

- Create a new container from that committed image instead of the default `docker_image`
- Apply the same mounts and port mappings as today

Idle auto-delete and `aivm destroy --keep-base` remove only the live container;
the committed base image is retained until explicitly invalidated or replaced.

## Deleting a broken or stale base

When the base must be discarded, aivm removes the backend artifact:

| Backend | What gets removed |
| --- | --- |
| **Lima** | Shadow instance `<name>-base` |
| **Docker** | Committed base image |

Base cleanup never deletes the **live** VM or container.

Delete the base when:

- Config hash changes (plugins, agents, VM settings that affect bootstrap)
- `BootstrapVersion` in `bootstrap-state.json` is stale
- Save or restore failed or timed out
- User runs full `aivm recreate` (without `--fast`)
- User runs `aivm destroy` (without `--keep-base`)
- User accepts a `vm.backend` change prompt
- `vm.type` changes (within the same backend)
- Base artifact expected but shadow instance or image is missing

If the user declines recreation after a config change, keep the running VM as
today — but delete the base so it cannot be restored incorrectly.

## Reliability

- **Never hang** on save, restore, or cleanup — use timeouts; then fall back
- **Save is best-effort** — if bootstrap succeeded but save failed, the VM is still usable; warn and try again next full bootstrap
- **Restore failure** — delete the base, create fresh, full bootstrap in the same `aivm start`
- **No base yet** — must work completely; indistinguishable from today except a save attempt after bootstrap

## Validation

Local smoke test (`scripts/lima-clone-smoke-test.sh`) on Lima with a minimal
bootstrap measured:

| Metric | Result |
| --- | --- |
| Cold create + bootstrap | 40s |
| Save base (stop + clone) | 13s |
| Fast recreate | 16s |
| Cold recreate | 47s |
| Speedup | **2.9×** (higher expected with real aivm plugins) |
| Disk after save | live and shadow both ~3.4 GiB (APFS copy-on-write) |

## Out of scope

- Base images shared across machines or published to a registry
- Base images that survive switching Lima ↔ Docker or changing `vm.type`
- Skipping bootstrap when configuration has changed
