# Integration Test Tier Design

## Summary

Add a **fast integration test tier** between unit tests and e2e tests. Integration
tests call `LifecycleService` directly with a stateful fake `vm.VM` that also
implements `vm.BaseImageStore`. No Docker, no subprocess — tests run in
milliseconds and exercise full orchestration paths (start, destroy, recreate,
decision engine, interactive prompts).

E2e tests remain for **Docker-specific behavior** (real commit/clone, plugin
install scripts, port binding, file copy). Integration tests own
**orchestration logic** that unit tests only cover in fragments.

## Problem

The current test pyramid has a wide gap:

| Tier | Speed | Coverage gap |
| --- | --- | --- |
| Unit | Fast | Pure functions and isolated methods (`EvaluateTimers`, individual prompts). Some lifecycle paths use local `captureVM` fakes but coverage is ad hoc. |
| E2e | Slow (minutes) | Full stack with real Docker containers via `aivm-test` subprocess. |
| Bootstrap | Slow | Real plugin/integration install scripts in containers. |

Orchestration scenarios — `decideStartAction` → `fastRecreate`, destroy with
`keepBase`, timer prompts, fallback to `fullBootstrap` — are either tested as
isolated pure functions (unit) or full stack (e2e). There is no middle tier that
wires `LifecycleService` end-to-end without Docker.

Goals (both equally important):

1. **Faster CI and local feedback** — cover orchestration branches without
   booting containers.
2. **Better coverage** — exercise decision paths, state-file side effects, error
   fallbacks, and interactive branches that e2e does not hit systematically.

## Decision

**Approach:** Dedicated integration tier (option 2) with shared fake
infrastructure.

- **Invocation model:** Direct service calls (approach A) — construct
  `LifecycleService`, call `Start()`, `Destroy()`, `Recreate()` directly.
- **Interactive flows:** `ScriptedConfirmer` wired through a test harness (no CLI
  subprocess required).
- **E2e boundary:** Keep e2e for Docker mechanics; integration owns orchestration
  (option B).

Rejected alternatives:

- **Grow unit tests in place** — fakes duplicate, unit vs orchestration boundary
  stays blurry.
- **Dual-mode e2e framework** — `test/framework` is subprocess/Docker-oriented;
  awkward fit for in-process service tests.

## Architecture

```text
┌─────────────────────────────────────────────────────────┐
│  test/integration/lifecycle/   (scenario tests)         │
│    TestFastRecreateAfterDestroyKeepBase                 │
│    TestBootstrapRefreshDeclined_FastRecreate              │
│    TestRuntimeChangeDeclined_PreservesBase                │
└────────────────────┬────────────────────────────────────┘
                     │ uses
┌────────────────────▼────────────────────────────────────┐
│  test/lifecycle/harness.go     (LifecycleHarness)       │
│    Builds LifecycleService with fakes + temp state dir  │
│    WithScriptedAnswers("2")  WithBootstrapDaysAgo(31)   │
└──────┬──────────────────────────────┬───────────────────┘
       │                              │
┌──────▼──────────┐          ┌────────▼─────────┐
│ test/testvm/    │          │ lifecycle.       │
│ FakeVM          │          │ ScriptedConfirmer│
│ - status FSM    │          │ SilentConfirmer  │
│ - base image    │          └──────────────────┘
│ - call log      │
│ implements VM + │
│ BaseImageStore  │
└─────────────────┘
```

### Test pyramid after this change

| Tier | What it proves | Docker? | Typical runtime |
| --- | --- | --- | --- |
| Unit | Pure functions, arg builders, individual prompt functions | No | ms |
| **Integration (new)** | `LifecycleService` orchestration, decisions, state files, interactive branches | No | ms–s |
| E2e | Real bootstrap, commit/clone, ports, file I/O in container | Yes | minutes |
| Bootstrap | Real plugin/integration install scripts | Yes | minutes |

## Production seam change

`AsBaseImageStore` currently type-switches on `*LimaVM` and `*DockerVM` only. A
fake VM cannot participate in base-image paths without a change.

Replace the type switch with an interface assertion:

```go
func AsBaseImageStore(v VM) (BaseImageStore, bool) {
    store, ok := v.(BaseImageStore)
    return store, ok
}
```

`*LimaVM` and `*DockerVM` already implement both `VM` and `BaseImageStore` on
the same struct. No production behavior change.

## FakeVM (`test/testvm`)

A **stateful simulator**, not a dumb stub. Models enough VM + base-image
behavior for lifecycle orchestration.

### State

- `status`: `NotFound` | `Stopped` | `Running`
- `baseImageExists`: whether a saved snapshot is present
- `callLog`: ordered record of method calls for assertions
- Optional fault injection: `RestoreErr`, `SaveErr`, `DestroyErr`, etc.

### Transitions

| Method | Effect |
| --- | --- |
| `Start` | `NotFound`/`Stopped` → `Running` |
| `Stop` | `Running` → `Stopped` |
| `Destroy` | live VM → `NotFound` (base image untouched) |
| `SaveBaseImage` | sets `baseImageExists = true` |
| `DeleteBaseImage` | sets `baseImageExists = false` |
| `RestoreFromBaseImage` | requires base; destroys live VM + starts (simulates fast recreate) |
| `HasBaseImage` | returns `baseImageExists` |
| `Run` / `RunOutput` / `RunStream` | no-op success; append script to `callLog` |
| `WaitReady` | no-op success |

### Defaults

- `Profile()` returns `"test"`
- `NeedsPortBindingAtBoot()` returns `true` (docker-like; only relevant if a
  test asserts port behavior)

`FakeVM` implements both `vm.VM` and `vm.BaseImageStore` on the same struct.

### Assertion API

- `CallLog()` — ordered method names or structured entries
- `SetStatus(s)` — pre-condition without calling Start
- `BaseImageExists()` — current base image flag
- `SetFault(opt)` — inject errors on specific methods

## LifecycleHarness (`test/lifecycle/harness`)

Builder that constructs a fully wired `LifecycleService` in `t.TempDir()`.

### Example

```go
h := lifecycleharness.New(t,
    lifecycleharness.WithScriptedAnswers("2"),
    lifecycleharness.WithBaseImageEnabled(true),
    lifecycleharness.WithBootstrapRefreshDays(30),
)
```

### Defaults (fast, focused)

| Dependency | Test double |
| --- | --- |
| `VM` | `testvm.FakeVM` |
| `Confirmer` | `SilentConfirmer` unless `WithScriptedAnswers` set |
| `Compose` | no-op stub |
| `T3Code` | `t3code.NoopManager` |
| `Monitor` | real struct with idle/delete timeouts disabled (`-1`) |
| `plugins.enabled` | empty — bootstrap runs but installs nothing |
| Config backend | `docker` (arbitrary; orchestration is backend-agnostic) |

### Options

- `WithScriptedAnswers(...string)` — sets `NewScriptedConfirmer`
- `WithBaseImageEnabled(bool)`
- `WithBootstrapRefreshDays(n)` / `WithRecreatePromptDays(n)`
- `WithBackend(string)` / `WithVMType(string)` — for runtime-change tests

### Seed helpers

Avoid needing a real first-boot for every scenario:

- `SeedBootstrapped()` — writes `bootstrap-state.json`, `bootstrap-at`,
  `vm-created-at` consistent with harness config hash/backend/vm_type
- `SetBootstrapDaysAgo(n)` / `SetVMCreatedDaysAgo(n)` — backdate epoch files
- `SetVMStatus(NotFound|Stopped|Running)` — pre-condition on fake

### Assertion helpers

- `SVC()` — `*lifecycle.LifecycleService`
- `VM()` — `*testvm.FakeVM`
- `StateDir()` — temp state directory
- `StateFileExists(name)` — host state file presence
- `BootstrapState()` — parsed `BootstrapState`
- `HasBaseImage()` — delegates to fake

Tests call `svc.Start(ctx)`, `svc.Destroy(ctx, keepBase)`,
`svc.Recreate(ctx, force, fast)` directly. No CLI subprocess.

## Interactive testing

`Confirmer` already abstracts interactive I/O. The harness wires it:

| Mode | Confirmer | Use for |
| --- | --- | --- |
| Non-interactive | `SilentConfirmer` | default CI paths, non-interactive `decideStartAction` |
| Interactive | `NewScriptedConfirmer("y")` / `("2")` / `("n")` | prompt branches |

**Division of responsibility:**

- `test/unit/lifecycle/dialog_test.go` — prompt function wording and parsing
- `test/integration/lifecycle/` — prompt → correct orchestration outcome

Integration tests assert **outcomes**: which path ran, state files, fake call
log. Example: combined prompt answer `"2"` → `RestoreFromBaseImage` in call log,
`bootstrap-at` unchanged, no `Destroy` before restore.

Optional later enhancement: redirect `aivmlog.Configure` stdout to a
`bytes.Buffer` for prompt text assertions in scenario tests. Not required for v1.

## Initial scenario list

### Integration tier (orchestration)

| Scenario | Key assertions |
| --- | --- |
| Destroy with `keepBase=true` | bootstrap state + base image preserved after live VM destroyed |
| Destroy with `keepBase=false` | base deleted, bootstrap state cleared |
| `Recreate(fast=true)` with valid base | `RestoreFromBaseImage` called; plugins not re-run (no bootstrap log) |
| `Recreate(fast=true)` without base | warn + `fullBootstrap` path |
| Restore failure | base deleted, fallback `fullBootstrap` |
| Non-interactive start, VM missing, valid base | fast recreate, no prompts |
| Bootstrap refresh accepted (`"y"`) | `fullBootstrap`, new `bootstrap-at` |
| Bootstrap refresh declined, VM stopped (`"n"`) | `fastRecreate` |
| Combined prompt options 1 / 2 / 3 | `fullBootstrap` / `fastRecreate` / resume respectively |
| Runtime change declined | no destroy, base preserved |
| Runtime change accepted | base deleted, full recreate |
| Config hash change | base deleted preemptively before prompt |
| VM age prompt accept with valid base | `fastRecreate` instead of full bootstrap |

### E2e tier (Docker mechanics — unchanged scope)

- Real `docker commit` and restore from committed image
- Plugin install scripts inside container
- Port binding, `cp`, compose health checks
- Agent launch markers in container
- One smoke test per major happy path

### Migration policy

1. Add integration tests first for orchestration scenarios above.
2. Do **not** delete e2e tests until integration suite covers the branches.
3. E2e count may shrink over time as confidence grows; Docker-specific tests
   remain permanently.

### Deprecating `ForTest` exports

Existing exports (`FastRecreateForTest`, `ApplyPostRestoreForTest`,
`RuntimeChangedForTest`) can remain for narrow unit tests. Prefer public service
API (`Start`, `Destroy`, `Recreate`) in integration tests. Remove `ForTest`
hooks when integration tests subsume their coverage.

## CI and Makefile

```makefile
test-integration:
 go test ./test/integration/... ./test/testvm/...
```

Add to CI workflow alongside `test-unit`. Both tiers are fast and do not require
Docker. Optionally run `test-integration` as part of the same job as `test-unit`
to reduce CI job count.

Update `.github/workflows/ci.yml` and `release.yml` with a
`make test-integration` step (or fold into unit job).

## File map

| File | Responsibility |
| --- | --- |
| `internal/vm/base_store.go` | Widen `AsBaseImageStore` to interface assertion |
| `test/testvm/fake.go` | `FakeVM` implementing `VM` + `BaseImageStore` |
| `test/testvm/fake_test.go` | FakeVM state machine unit tests |
| `test/lifecycle/harness.go` | `LifecycleHarness` builder and seed/assert helpers |
| `test/integration/lifecycle/start_test.go` | Start + decision engine scenarios |
| `test/integration/lifecycle/destroy_test.go` | Destroy keepBase scenarios |
| `test/integration/lifecycle/recreate_test.go` | Recreate fast/full scenarios |
| `test/integration/lifecycle/prompts_test.go` | Interactive prompt → outcome scenarios |
| `Makefile` | `test-integration` target |

## Non-goals

- CLI subprocess tests (Cobra flag parsing) — out of scope for v1; e2e covers CLI
  smoke; add a thin CLI layer later only if needed.
- Replacing bootstrap tests (`-tags bootstrap`) — those validate real install
  scripts.
- Replacing all e2e tests — Docker mechanics stay in e2e.
- Mocking plugin.Executor with scripted installs — empty plugin list is
  sufficient for orchestration tests.

## Success criteria

- `make test-integration` completes in under 10 seconds on a typical dev machine.
- Every `RecreationAction` branch in `decideStartAction` / `handleRecreationPrompt`
  has at least one integration test.
- Base image orchestration paths (`fastRecreate`, fallback, `keepBase`) covered
  without Docker.
- CI runs integration tests on every PR without Docker-in-Docker.
- E2e suite unchanged in scope for v1; no e2e tests removed until integration
  parity is verified.
