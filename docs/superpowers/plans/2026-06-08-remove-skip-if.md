# Remove skip_if Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove `skip_if` from plugins, agents, and integrations; always run
`setup`/`configure` on bootstrap; drop the unused in-VM bootstrap marker and
dead `LifecycleService.Bootstrap` method; route stale host bootstrap state to
VM recreate instead of reconcile.

**Architecture:** Bootstrap becomes unconditional: `Executor.Run(ctx)` runs
every enabled plugin's `setup` in DAG order (with retries only). Integrations
always run `configure` when `from`/`to` match. Host-side
`bootstrap-state.json` still gates whether bootstrap runs at all (`syncBootstrap`
up-to-date check). Stale/missing host state triggers `recreateVM` instead of
`fullBootstrap(force=false)`. In-VM `~/.aivm-bootstrap-version` file is removed
(never read). `BootstrapVersion` stays on host state only; bump to `"3"` for
this breaking change.

**Tech Stack:** Go, YAML plugin definitions, Docker bootstrap tests
(`-tags bootstrap`), e2e tests (`-tags integration`)

---

## File map

| File | Change |
| --- | --- |
| `internal/plugin/plugin.go` | Remove `SkipIf` from `Plugin` interface |
| `internal/plugin/executor.go` | Remove `force` param and all `SkipIf` calls |
| `internal/plugin/yaml_plugin.go` | Remove `SkipIf` field, merge logic, `SkipIf()` method |
| `internal/plugin/mise_plugin.go` | Remove `skipIfScript()` and `SkipIf()` |
| `internal/agent/def.go` | Remove `SkipIf` field and merge/`ToPluginDef` mapping |
| `internal/integration/integration.go` | Remove `SkipIf` field and executor check |
| `internal/bootstrap/bootstrap.go` | Remove `force` param, in-VM marker write; bump `BootstrapVersion` to `"3"` |
| `internal/lifecycle/bootstrap.go` | Rename `fullBootstrap` → `bootstrap`, drop `force` |
| `internal/lifecycle/sync.go` | `missingOrStaleStep` calls `recreateVM` |
| `internal/lifecycle/service.go` | Use `bootstrap()`; delete `Bootstrap()` method |
| `internal/agent/defaults.yaml` | Remove all `skip_if` blocks |
| `internal/plugin/defaults.yaml` | Remove all `skip_if` blocks |
| `test/bootstrap/harness_test.go` | Remove `AssertSkipIf`; `Install` calls `Run(ctx)` |
| `test/bootstrap/*_test.go` | Remove `AssertSkipIf` calls (keep `AssertCommand`) |
| `test/unit/**` | Remove skip_if assertions and mise SkipIf tests |
| `test/e2e/bootstrap_sync_test.go` | Rewrite version-mismatch test for recreate path |
| `README.md`, `aivm.example.yaml` | Remove skip_if docs and examples |

---

### Task 1: Plugin interface and executor

**Files:**

- Modify: `internal/plugin/plugin.go`
- Modify: `internal/plugin/executor.go`

- [ ] **Step 1: Remove SkipIf from Plugin interface**

In `internal/plugin/plugin.go`, delete the `SkipIf` method from the interface:

```go
// Plugin is the contract every bootstrap component must satisfy.
type Plugin interface {
    Name() string
    Description() string
    Dependencies() []string
    Agents() []string
    PathEntries() []string
    Setup(ctx context.Context, env InstallEnv) error
}
```

- [ ] **Step 2: Simplify executor — remove force and skip_if**

Replace `internal/plugin/executor.go` `Run` and `installPlugin` signatures and bodies:

```go
// Run executes all enabled plugins in DAG order.
func (e *Executor) Run(ctx context.Context) error {
    ordered, err := e.Ordered()
    if err != nil {
        return fmt.Errorf("resolving plugin order: %w", err)
    }

    if err := e.writePathFile(ctx, ordered); err != nil {
        return fmt.Errorf("writing path file: %w", err)
    }

    explicitlyEnabled := make(map[string]bool, len(e.Enabled))
    for _, name := range e.Enabled {
        explicitlyEnabled[name] = true
    }

    dependents := make(map[string][]string)
    var collectDeps func(root, name string)
    collectDeps = func(root, name string) {
        p, ok := e.Registry.Get(name)
        if !ok {
            return
        }
        for _, dep := range p.Dependencies() {
            if !explicitlyEnabled[dep] {
                already := false
                for _, r := range dependents[dep] {
                    if r == root {
                        already = true
                        break
                    }
                }
                if !already {
                    dependents[dep] = append(dependents[dep], root)
                }
            }
            collectDeps(root, dep)
        }
    }
    for _, name := range e.Enabled {
        collectDeps(name, name)
    }

    for _, p := range ordered {
        if err := ctx.Err(); err != nil {
            return err
        }
        if err := e.installPlugin(
            ctx, p, explicitlyEnabled, dependents,
        ); err != nil {
            return err
        }
    }
    return nil
}

func (e *Executor) installPlugin(
    ctx context.Context, p Plugin,
    explicitlyEnabled map[string]bool, dependents map[string][]string,
) error {
    return aivmlog.WithWriter(p.Name(), func(logW io.Writer) error {
        if !explicitlyEnabled[p.Name()] {
            if roots := dependents[p.Name()]; len(roots) > 0 {
                slog.Info(fmt.Sprintf(
                    "auto-installing %s (required by: %s)",
                    p.Name(), joinNames(roots),
                ))
            }
        }

        cfg := e.PluginConfig[p.Name()]
        if cfg == nil {
            cfg = map[string]any{}
        }
        env := InstallEnv{
            Config:   cfg,
            StateDir: e.StateDir,
            Log:      logW,
            VM:       e.VMInst,
        }

        slog.Info(fmt.Sprintf("Plugin: %s", p.Name()))
        start := time.Now()

        var setupErr error
        for attempt := 1; attempt <= maxSetupRetries; attempt++ {
            if err := ctx.Err(); err != nil {
                return err
            }

            if attempt > 1 {
                delay := setupRetryDelay(attempt)
                slog.Warn(fmt.Sprintf(
                    "setup %s failed (attempt %d/%d): %v — retrying in %s...",
                    p.Name(), attempt-1, maxSetupRetries, setupErr, delay,
                ))
                select {
                case <-time.After(delay):
                case <-ctx.Done():
                    return ctx.Err()
                }
            }

            setupErr = p.Setup(ctx, env)
            if setupErr == nil {
                break
            }
        }

        if setupErr != nil {
            return fmt.Errorf("setup %s: %w", p.Name(), setupErr)
        }

        slog.Info(fmt.Sprintf("%s set up (%s)", p.Name(), time.Since(start).Round(time.Second)))
        return nil
    })
}
```

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: FAIL — `YAMLPlugin` and `MisePlugin` still implement removed
`SkipIf` (fixed in Task 2)

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/plugin.go internal/plugin/executor.go
git commit -m "refactor: remove skip_if and force from plugin executor"
```

---

### Task 2: Plugin implementations

**Files:**

- Modify: `internal/plugin/yaml_plugin.go`
- Modify: `internal/plugin/mise_plugin.go`

- [ ] **Step 1: Remove SkipIf from YAMLPlugin**

In `internal/plugin/yaml_plugin.go`:

- Delete `SkipIf` field from `PluginDef` struct
- Delete `SkipIf` merge block in `MergePluginDef`
- Delete entire `func (p *YAMLPlugin) SkipIf(...)` method
- Update comment on `TemplateFuncMap` to say "setup scripts" only

- [ ] **Step 2: Remove SkipIf from MisePlugin**

In `internal/plugin/mise_plugin.go`, delete `skipIfScript()` and `SkipIf()`
entirely. Keep only `Setup()`.

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: PASS (agent/integration/lifecycle may still reference SkipIf —
fixed in later tasks)

- [ ] **Step 4: Commit**

```bash
git add internal/plugin/yaml_plugin.go internal/plugin/mise_plugin.go
git commit -m "refactor: remove skip_if from yaml and mise plugins"
```

---

### Task 3: Agent and integration types

**Files:**

- Modify: `internal/agent/def.go`
- Modify: `internal/integration/integration.go`

- [ ] **Step 1: Remove SkipIf from agent Def**

In `internal/agent/def.go`:

- Delete `SkipIf` field from `Def`
- Remove `SkipIf` from `ToPluginDef()`
- Remove `SkipIf` block from `MergeDef()`

- [ ] **Step 2: Remove SkipIf from integration**

In `internal/integration/integration.go`:

- Delete `SkipIf` field from `IntegrationDef`
- In `Run()`, delete the skip_if check block (lines checking `integ.SkipIf`);
  go straight to rendering and running `configure`
- Update `Run` doc comment to: "executes all matching integrations in order"

```go
    for _, integ := range matching {
        if err := ctx.Err(); err != nil {
            return ran, err
        }

        script, err := renderScript(integ.Configure, e.TemplateVars)
        if err != nil {
            return ran, fmt.Errorf(
                "integration %s: render script: %w", integ.Key(), err,
            )
        }
        if e.VM != nil {
            if err := e.VM.Run(ctx, script, nil); err != nil {
                return ran, fmt.Errorf("integration %s: %w", integ.Key(), err)
            }
        }
        ran = append(ran, integ.Key())
    }
```

- [ ] **Step 3: Commit**

```bash
git add internal/agent/def.go internal/integration/integration.go
git commit -m "refactor: remove skip_if from agent and integration defs"
```

---

### Task 4: Bootstrap engine — drop marker and force

**Files:**

- Modify: `internal/bootstrap/bootstrap.go`

- [ ] **Step 1: Simplify Engine.Run and remove in-VM marker**

Replace contents of `internal/bootstrap/bootstrap.go`:

```go
package bootstrap

import (
    "context"
    "log/slog"

    "github.com/sisimomo/aivm/internal/plugin"
    "github.com/sisimomo/aivm/internal/vm"
)

// BootstrapVersion is incremented whenever host bootstrap-state schema or
// bootstrap behaviour changes. Stored in bootstrap-state.json on the host only.
const BootstrapVersion = "3"

// Engine orchestrates VM bootstrap: it runs all configured plugins via Executor.
type Engine struct {
    VM       vm.VM
    Executor *plugin.Executor
    StateDir string
}

// Run executes all configured plugins.
func (e *Engine) Run(ctx context.Context) error {
    slog.Info("Bootstrapping VM")

    if err := e.Executor.Run(ctx); err != nil {
        return err
    }

    slog.Info("Bootstrap complete!")
    return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/bootstrap/bootstrap.go
git commit -m "refactor: drop in-VM bootstrap marker and force flag from engine"
```

---

### Task 5: Lifecycle — bootstrap, sync, delete dead Bootstrap method

**Files:**

- Modify: `internal/lifecycle/bootstrap.go`
- Modify: `internal/lifecycle/sync.go`
- Modify: `internal/lifecycle/service.go`

- [ ] **Step 1: Simplify lifecycle bootstrap**

In `internal/lifecycle/bootstrap.go`, rename and simplify:

```go
// bootstrap runs all configured plugins on targetVM and saves bootstrap state
// on success.
func (svc *LifecycleService) bootstrap(
    ctx context.Context, targetVM vm.VM,
) error {
    eng := svc.newBootstrapEngine(targetVM, nil)
    if err := eng.Run(ctx); err != nil {
        return err
    }
    if err := applyVMEnv(
        ctx, targetVM, svc.Config.VM.ResolvedEnv(),
    ); err != nil {
        return fmt.Errorf("applying vm.env: %w", err)
    }
    gitName, gitEmail := readHostGitIdentity()
    if err := applyGitIdentity(ctx, targetVM, gitName, gitEmail); err != nil {
        return fmt.Errorf("applying git identity: %w", err)
    }
    if err := svc.recordBootstrapState(); err != nil {
        return err
    }
    return svc.runIntegrationsFromState(ctx, targetVM)
}
```

Update `runIntegrationsFromState` comment — remove skip_if mention.

- [ ] **Step 2: Stale state → recreate**

In `internal/lifecycle/sync.go`, change `missingOrStaleStep.run`:

```go
func (s *missingOrStaleStep) run(
    ctx context.Context, _ *syncState, svc *LifecycleService,
) error {
    svc.logger().Warn("bootstrap state missing or outdated — recreating VM")
    return svc.recreateVM(ctx)
}
```

- [ ] **Step 3: Update service.go**

In `internal/lifecycle/service.go`:

- `ensureBootstrapped`: change `svc.fullBootstrap(ctx, svc.VM, true)` to
  `svc.bootstrap(ctx, svc.VM)`
- Delete entire `Bootstrap(ctx, onlyPlugin, force)` method (lines 551–571)

- [ ] **Step 4: Verify compile**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/lifecycle/bootstrap.go internal/lifecycle/sync.go \
  internal/lifecycle/service.go
git commit -m "refactor: unconditional bootstrap, stale state recreates VM"
```

---

### Task 6: YAML defaults — remove skip_if blocks

**Files:**

- Modify: `internal/agent/defaults.yaml`
- Modify: `internal/plugin/defaults.yaml`

- [ ] **Step 1: Strip skip_if from agent defaults**

Remove every `skip_if:` block from `internal/agent/defaults.yaml` (claude,
copilot, cursor, opencode). Each agent keeps only `setup:` and other fields.

Example — claude after edit:

```yaml
claude:
  description: "Claude Code (Anthropic)"
  dependencies:
    - mise-node
  path_entries:
    - "$HOME/.claude/local/bin"
  persist:
    - .claude/projects
  cli_command: claude
  launch_args: --dangerously-skip-permissions
  setup: |
    curl -fsSL https://claude.ai/install.sh | bash
```

- [ ] **Step 2: Strip skip_if from plugin defaults**

Remove every `skip_if:` block from `internal/plugin/defaults.yaml` (system,
mise, awscli, cocoindex-code, context7, t3code). Keep `skills` unchanged (it
already has no `skip_if`).

For `cocoindex-code`, the `skip_if` had a template branch for `.config` — that
logic is gone; `setup` already writes the config file when `.config` is set.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/defaults.yaml internal/plugin/defaults.yaml
git commit -m "refactor: remove skip_if from embedded agent and plugin defaults"
```

---

### Task 7: Unit tests

**Files:**

- Modify: `test/unit/agent/defaults_test.go`
- Modify: `test/unit/plugin/defaults_test.go`
- Modify: `test/unit/plugin/mise_plugin_test.go`
- Modify: `test/unit/plugin/skills_plugin_test.go`
- Modify: `test/unit/integration/defaults_test.go`

- [ ] **Step 1: Update agent defaults_test**

In `test/unit/agent/defaults_test.go`:

- Delete the `if def.SkipIf == ""` check in `TestLoadDefs_AllAgentsPresent`
- In `TestLoadDefs_ScriptsAreValidTemplates`, remove the `def.SkipIf` template
  parse loop; keep only `setup`
- In `TestLoadDefs_ToPluginDef`, remove the `pd.SkipIf != def.SkipIf` assertion

- [ ] **Step 2: Update plugin defaults_test**

In `test/unit/plugin/defaults_test.go`:

- Delete `if def.SkipIf == ""` check in `TestLoadDefaults_AllPluginsPresent`
- In `TestLoadDefaults_ScriptsAreValidTemplates`, remove `def.SkipIf` loop

- [ ] **Step 3: Remove mise SkipIf tests**

In `test/unit/plugin/mise_plugin_test.go`, delete:

- `wantSkipIfScript` function
- `TestMisePlugin_SkipIf_DefaultVersion`
- `TestMisePlugin_SkipIf_PinnedVersion`
- `TestMisePlugin_SkipIf_MultiVersion`
- `TestMisePlugin_SkipIf_MultiVersion_YAMLSlice`
- `TestMisePlugin_SkipIf_NotInstalled`

Keep all `TestMisePlugin_Setup_*` tests.

- [ ] **Step 4: Delete skills NoSkipIf test**

Delete entire `TestSkillsPlugin_NoSkipIf` from `test/unit/plugin/skills_plugin_test.go`.

- [ ] **Step 5: Update integration defaults_test**

In `test/unit/integration/defaults_test.go`, rename test to
`TestLoadDefaults_ConfigureTemplatesCompile` and remove the `d.SkipIf` loop;
keep only `configure` template parsing.

- [ ] **Step 6: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add test/unit/agent/defaults_test.go test/unit/plugin/defaults_test.go \
  test/unit/plugin/mise_plugin_test.go test/unit/plugin/skills_plugin_test.go \
  test/unit/integration/defaults_test.go
git commit -m "test: remove skip_if assertions from unit tests"
```

---

### Task 8: Bootstrap harness and Docker tests

**Files:**

- Modify: `test/bootstrap/harness_test.go`
- Modify: `test/bootstrap/claude_test.go`
- Modify: `test/bootstrap/copilot_test.go`
- Modify: `test/bootstrap/cursor_test.go`
- Modify: `test/bootstrap/opencode_test.go`
- Modify: `test/bootstrap/system_test.go`
- Modify: `test/bootstrap/awscli_test.go`
- Modify: `test/bootstrap/mise_test.go`
- Modify: `test/bootstrap/cocoindex_test.go`
- Modify: `test/bootstrap/context7_test.go`
- Modify: `test/bootstrap/t3code_test.go`
- Modify: `test/bootstrap/multi_agent_test.go`

- [ ] **Step 1: Update harness**

In `test/bootstrap/harness_test.go`:

- Change `exec.Run(context.Background(), true)` to `exec.Run(context.Background())`
- Delete entire `AssertSkipIf` function (lines 243–279)
- Delete `AssertIntegrationConfigured` function (depends on integration
  `skip_if`) OR rewrite it to run a caller-supplied verify command — simplest:
  delete it (no callers in repo today)
- Update `RunIntegrations` doc comment — remove skip_if idempotency notes

- [ ] **Step 2: Remove AssertSkipIf from all bootstrap tests**

In each file below, delete `h.AssertSkipIf(...)` lines and update file
comments to remove skip_if/idempotency mentions:

- `test/bootstrap/claude_test.go`
- `test/bootstrap/copilot_test.go`
- `test/bootstrap/cursor_test.go`
- `test/bootstrap/opencode_test.go`
- `test/bootstrap/system_test.go`
- `test/bootstrap/awscli_test.go`
- `test/bootstrap/mise_test.go` (also update struct comment on
  `mise-standalone` case)
- `test/bootstrap/cocoindex_test.go`
- `test/bootstrap/context7_test.go`
- `test/bootstrap/t3code_test.go`
- `test/bootstrap/multi_agent_test.go` (rewrite comment: both agents installed
  in one bootstrap via DAG, not skip_if)

- [ ] **Step 3: Run bootstrap tests**

Run: `make test-bootstrap RUN='TestPlugin_|TestAgent_'`
Expected: PASS (requires Docker)

- [ ] **Step 4: Commit**

```bash
git add test/bootstrap/
git commit -m "test: remove skip_if harness helpers and bootstrap assertions"
```

---

### Task 9: E2E bootstrap sync test

**Files:**

- Modify: `test/e2e/bootstrap_sync_test.go`

- [ ] **Step 1: Rewrite version-mismatch test**

Replace `TestStartRerunBootstrapAfterVersionMismatch` with a recreate expectation:

```go
// TestStartRecreatesVMAfterVersionMismatch verifies that a stale bootstrap
// state (wrong version) triggers VM recreation instead of in-place reconcile.
//
//  1. First start: bootstrap runs, state recorded.
//  2. Corrupt the state's version field to simulate an old format.
//  3. Second start: version mismatch triggers recreateVM.
func TestStartRecreatesVMAfterVersionMismatch(t *testing.T) {
    t.Parallel()
    h := framework.New(t,
        framework.WithInteractive("y"), // not used — recreateVM does not prompt
    )

    h.Scenario("stale bootstrap version triggers VM recreation").
        Step("Start VM (first boot)", actions.CLI("start")).
        Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
        Assert("Bootstrap state recorded", assertions.BootstrapComplete()).
        Step("Corrupt bootstrap state version to simulate an upgrade", actions.CorruptBootstrapVersion()).
        Step("Reset output buffer", actions.ResetOutput()).
        Step("Start VM again — version mismatch triggers recreation", actions.CLI("start")).
        Wait("VM is running after recreation",
            conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
        Assert("Bootstrap state is valid again", assertions.BootstrapComplete()).
        Assert("User saw stale-state warning",
            assertions.StderrContains("bootstrap state missing or outdated")).
        Assert("User saw VM recreated message",
            assertions.OutputContains("VM recreated")).
        Run()
}
```

- [ ] **Step 2: Run e2e sync tests**

Run: `make test-e2e RUN=Bootstrap`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add test/e2e/bootstrap_sync_test.go
git commit -m "test: stale bootstrap state expects VM recreate not reconcile"
```

---

### Task 10: Documentation

**Files:**

- Modify: `README.md`
- Modify: `aivm.example.yaml`

- [ ] **Step 1: Update README**

In `README.md`, remove all `skip_if` references:

- Integrations section: delete `skip_if` from example YAML and prose ("Each
  integration's skip_if script gates re-runs")
- Plugin schema table: remove `skip_if` row
- Custom plugin section: remove `skip_if` from example and schema table
- Config-driven installs: remove `skip_if` template example; keep `setup` templates
- Agent define section: remove `skip_if / setup` override bullet
- Remove "After changing setup or skip_if, run aivm recreate" — replace with
  "After changing setup, run aivm recreate"
- Add one line under plugin authoring: "`setup` scripts must be safe to run on a
  fresh VM (use idempotent installers)."

- [ ] **Step 2: Update aivm.example.yaml**

Remove commented `skip_if` blocks from plugin define example and integrations example.

- [ ] **Step 3: Lint markdown**

Run: `npx markdownlint-cli2 "README.md" --fix`

- [ ] **Step 4: Commit**

```bash
git add README.md aivm.example.yaml
git commit -m "docs: remove skip_if from README and example config"
```

---

### Task 11: Final verification

**Files:** (none — verification only)

- [ ] **Step 1: Full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 2: Vet and build**

Run: `go vet ./... && go build -o bin/aivm ./cmd/aivm`
Expected: PASS

- [ ] **Step 3: E2E tests (if Docker/Colima available)**

Run: `make test-e2e`
Expected: PASS

- [ ] **Step 4: Bootstrap tests (if Docker available)**

Run: `make test-bootstrap`
Expected: PASS

---

## Self-review

**Spec coverage:**

- [x] Remove skip_if from plugins, agents, integrations — Tasks 1–3, 6
- [x] Remove force flag — Tasks 1, 4, 5
- [x] Stale state → recreate (not reconcile) — Task 5
- [x] Drop in-VM marker (optional 1) — Task 4
- [x] Delete LifecycleService.Bootstrap (optional 3) — Task 5
- [x] Keep config-change prompt with continue option (NOT optional 2) —
  unchanged in sync.go
- [x] Tests updated — Tasks 7–9
- [x] Docs updated — Task 10
- [x] BootstrapVersion bumped — Task 4

**Placeholder scan:** None found.

**Type consistency:** `Executor.Run(ctx)`, `Engine.Run(ctx)`,
`bootstrap(ctx, vm)` used consistently across tasks.
