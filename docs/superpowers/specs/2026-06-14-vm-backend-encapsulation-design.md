# VM Backend Encapsulation Design

## Summary

Tighten the seam between `internal/lifecycle` and VM backends (Lima, Docker) so
lifecycle orchestration never branches on backend name or concrete VM types.
Backend-specific behavior (two-phase Docker mounts, host-dir permissions, SSH
cleanup, post-bootstrap finalization) moves behind new `vm.VM` methods.

Config may still read `vm.backend` from YAML (user choice). `mountspec` becomes
fully backend-agnostic.

## Goals

- Lifecycle has **zero** `effectiveBackend() == "docker"` checks for orchestration
- Lifecycle has **zero** `*vm.DockerVM` / `*vm.LimaVM` type assertions
- Delete `internal/lifecycle/docker_promote.go`; promotion lives on `DockerVM`
- `mountspec` does not import or branch on backend names
- Preserve current mount-system behavior on `support/refactor-mounts` (no
  functional regression)
- Minimal diff (~8 files, mostly code moved not invented)

## Non-Goals

- Unifying Lima/Docker base-image storage internals
- Changing mount assembly (`ResolvedMountsForBootstrap` / `ResolvedMountsForRuntime`)
- Plugin, agent, or config schema changes
- Dynamic backend plug-in registry
- Resolving the `TEMP FIX` commit (separate follow-up)

## Background: leaks on `support/refactor-mounts`

The mount refactor correctly split bootstrap vs runtime mount profiles for
Docker, but orchestration leaked into lifecycle:

| Leak | Location | Today |
| --- | --- | --- |
| Backend string branch | `service.go`, `bootstrap_paths.go`, `bootstrap.go`, `helpers.go` | `effectiveBackend() == "docker"` for mount phase and promotion |
| Concrete type assert | `docker_promote.go` | `*vm.DockerVM` → `PromoteWithEphemeralCommit` |
| Concrete type assert | `bootstrap.go` | `*vm.LimaVM` → `CloseSSHControlMaster` |
| Docker permissions | `helpers.go` | `ensureHostMountDir(..., dockerBackend bool)` |
| Backend in mountspec | `mountspec/vmhome.go` | `DefaultVMHome(backend, …)` branches on `"docker"` |

Existing good abstractions to keep: `vm.VM`, `NeedsPortBindingAtBoot`,
`GetPublishedPort`, `BaseImageStore` / `AsBaseImageStore`.

## Boundary rules

| Layer | May know backend? | Responsibility |
| --- | --- | --- |
| `internal/vm` | Yes | All Lima/Docker mechanics |
| `internal/lifecycle` | **No** (for behavior) | Orchestrate via `vm.VM` interface only |
| `internal/config` | Yes (`vm.backend` YAML) | Parse config, derive `ParsedVMHome`, validate |
| `internal/mountspec` | **No** | Path resolution/validation given `vmHome` string |

`effectiveBackend()` may remain in lifecycle **only** for persisted metadata:
bootstrap state, recreation prompts, base-image validity checks (comparing stored
labels to current config). It must not drive mount orchestration or host-dir prep.

## Architecture

```text
config (vm.backend YAML)
    │
    ▼
vm.NewFromConfig ──► vm.VM implementation (LimaVM | DockerVM)
    │
    ▼
lifecycle orchestration
    ├─ buildBootstrapStartOptions / buildRuntimeStartOptions  (backend-agnostic)
    ├─ VM.UsesBootstrapOnlyMounts()  → pick which opts at create
    ├─ VM.PrepareHostMountDir()      → host persistence dirs
    ├─ VM.Start(opts)
    ├─ bootstrap() plugins
    ├─ VM.AfterBootstrapPlugins()
    └─ VM.FinalizeAfterBootstrap(runtimeOpts)  when UsesBootstrapOnlyMounts
```

### Uniform lifecycle flow

**First start / full bootstrap:**

```text
ensureAgentMountDirs → VM.PrepareHostMountDir (per path)

opts := UsesBootstrapOnlyMounts ? bootstrapOpts : runtimeOpts
VM.Start(opts)
VM.WaitReady
bootstrap()              # plugins: bootstrap profile (Docker) or full (Lima)
VM.AfterBootstrapPlugins()
if UsesBootstrapOnlyMounts:
    VM.FinalizeAfterBootstrap(runtimeOpts)
Compose.Up
```

**Resume / subsequent start:** unchanged — `buildRuntimeStartOptions`, no
finalization step.

**Lima finalize (inside `FinalizeAfterBootstrap`):** when `base_image_enable`,
`SaveBaseImage` with `runtimeOpts` (today's tail of `bootstrap()`).

**Docker finalize (inside `FinalizeAfterBootstrap`):** today's
`promoteDockerToRuntimeMounts` — `SaveBaseImage` + `RestoreFromBaseImage` when
base images enabled, else ephemeral commit/recreate via `PromoteWithEphemeralCommit`.

## `vm.VM` interface extensions

Add to `internal/vm/vm.go`:

```go
// UsesBootstrapOnlyMounts reports whether the initial VM create should attach
// only bootstrap mounts (user vm.mounts). Agent and T3 mounts are applied
// later via FinalizeAfterBootstrap. True for Docker; false for Lima.
UsesBootstrapOnlyMounts() bool

// PrepareHostMountDir creates a host directory suitable for bind-mounting into
// this backend (e.g. permissive permissions when guest UID differs from host).
PrepareHostMountDir(hostPath string) error

// AfterBootstrapPlugins runs backend-specific cleanup immediately after plugin
// bootstrap completes. Lima closes the SSH control master; Docker is a no-op.
AfterBootstrapPlugins(ctx context.Context) error

// FinalizeAfterBootstrap transitions from bootstrap to runtime configuration
// after plugins succeed. Docker recreates the container with runtimeOpts; Lima
// saves the base image when enabled. Called only when UsesBootstrapOnlyMounts
// is true at first-create time; safe no-op when not needed.
FinalizeAfterBootstrap(ctx context.Context, runtimeOpts StartOptions) error
```

### Per-backend behavior

| Method | Lima | Docker |
| --- | --- | --- |
| `UsesBootstrapOnlyMounts` | `false` | `true` |
| `PrepareHostMountDir` | `MkdirAll 0755` | `MkdirAll 0755` + `chmod 0777` |
| `AfterBootstrapPlugins` | `CloseSSHControlMaster` | no-op |
| `FinalizeAfterBootstrap` | `SaveBaseImage` if enabled | promote (commit + recreate with runtimeOpts) |

`PromoteWithEphemeralCommit` stays on `DockerVM` as an unexported helper called
from `FinalizeAfterBootstrap`. Not referenced from lifecycle.

## Code changes

| File | Change |
| --- | --- |
| `internal/vm/vm.go` | Add four interface methods |
| `internal/vm/lima.go` | Implement new methods; move SSH cleanup from lifecycle |
| `internal/vm/docker.go` | Implement `PrepareHostMountDir`, no-op hooks |
| `internal/vm/docker_base.go` | `FinalizeAfterBootstrap` calls promote logic |
| `internal/vm/home.go` | **New** — `DefaultUserHome(backend, hostHome)` moved from mountspec |
| `internal/mountspec/vmhome.go` | **Delete** — logic moved to `vm/home.go` |
| `internal/config/config.go` | Call `vm.DefaultUserHome` instead of `mountspec.DefaultVMHome` |
| `internal/lifecycle/helpers.go` | `ensureAgentMountDirs` takes `vm.VM`; drop `dockerBackend` param |
| `internal/lifecycle/service.go` | Replace docker branches with VM methods |
| `internal/lifecycle/bootstrap_paths.go` | Same |
| `internal/lifecycle/bootstrap.go` | Remove Lima assert and docker base-image branch |
| `internal/lifecycle/docker_promote.go` | **Delete** |
| `test/unit/lifecycle/docker_promote_test.go` | Move to `test/unit/vm/docker_finalize_test.go` (or similar) |
| `test/unit/mountspec/vmhome_test.go` | Move to `test/unit/vm/home_test.go` |

## Error handling

- `FinalizeAfterBootstrap` errors propagate to the caller (start/full-bootstrap
  fails visibly, same as today's promote failure).
- `SaveBaseImage` failures inside Lima `FinalizeAfterBootstrap` log a warning
  and continue (same as `SaveBaseImageBestEffort` today).
- `PrepareHostMountDir` returns wrapped errors with the host path (same as today).

## Testing

1. **VM unit tests** — `FinalizeAfterBootstrap` on Docker (with/without base
   image), Lima save-base-image path, `PrepareHostMountDir` permissions,
   `UsesBootstrapOnlyMounts` constants.
2. **Lifecycle unit tests** — fake `vm.VM` stub implementing new methods; assert
   call order (Start → bootstrap → AfterBootstrapPlugins → Finalize) without
   backend strings.
3. **Existing e2e/integration** — no changes expected; run mount and bootstrap
   suites on both backends.

## Migration / rollout

Single PR on `support/refactor-mounts`. No config changes. No user-facing
behavior change when switching `vm.backend` between `lima` and `docker`.

## Alternatives considered

| Approach | Why not |
| --- | --- |
| Separate `vm.Strategy` object | Two objects to wire through lifecycle; bigger refactor |
| Capability flags struct | Lifecycle still encodes backend semantics behind renamed flags |
| Do nothing | Mount work added 6+ backend branches; next backend would multiply leaks |
