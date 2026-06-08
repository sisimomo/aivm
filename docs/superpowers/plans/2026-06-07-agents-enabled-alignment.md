# Agents Enabled Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `agents.define.<name>.enable` with `agents.enabled` list,
matching the plugin enablement model while keeping `define` for overrides only.

**Architecture:** Add `Enabled []string` to `AgentsConfig` and rewrite
`ActiveAgents()` to return a deduplicated sorted list from it. Remove the
`Enable` field from `agent.Def`. Update composition error messages and
validation. Bootstrap path is unchanged — enabled agent defs still merge into
the plugin registry.

**Tech Stack:** Go, Viper/mapstructure YAML config, existing unit/e2e test
harness

**Spec:** `docs/superpowers/specs/2026-06-07-agents-enabled-alignment-design.md`

---

## File map

| File | Change |
| --- | --- |
| `internal/config/config.go` | Add `AgentsConfig.Enabled`; rewrite `ActiveAgents()` |
| `internal/agent/def.go` | Remove `Enable` field and merge logic |
| `internal/agent/defaults.yaml` | Remove `enable: false` lines |
| `internal/config/composition.go` | Update error messages; allow custom agents via `define` |
| `internal/lifecycle/service.go` | Update comment |
| `test/unit/config/agents_test.go` | Rewrite tests for `agents.enabled` |
| `test/unit/config/composition_test.go` | Migrate YAML fixtures to `agents.enabled` |
| `test/unit/lifecycle/hash_test.go` | Migrate inline YAML to `agents.enabled` |
| `test/framework/config.go` | Generate `agents.enabled` instead of `define.enable` |
| `aivm.example.yaml` | Show `agents.enabled` in examples |
| `README.md` | Update Agents section and quick-start snippets |
| `demo/configs/aivm.yaml` | Migrate to `agents.enabled` |
| `test/e2e/multi_agent_test.go` | Update scenario comments |

---

### Task 1: `ActiveAgents()` reads from `agents.enabled`

**Files:**

- Modify: `internal/config/config.go`
- Test: `test/unit/config/agents_test.go`

- [ ] **Step 1: Rewrite failing unit tests**

Replace the `ActiveAgents` tests and add dedupe coverage. Delete
`TestActiveAgents_NoneEnabledInDefine`. Rename/update the rest:

```go
func TestActiveAgents_EmptyConfig(t *testing.T) {
    t.Parallel()
    cfg := &config.Config{}
    got := cfg.ActiveAgents()
    if len(got) != 0 {
        t.Errorf("ActiveAgents() with empty config: got %v, want empty", got)
    }
}

func TestActiveAgents_SingleEnabled(t *testing.T) {
    t.Parallel()
    cfg := &config.Config{
        Agents: config.AgentsConfig{
            Enabled: []string{"claude"},
        },
    }
    got := cfg.ActiveAgents()
    if len(got) != 1 || got[0] != "claude" {
        t.Errorf("ActiveAgents() = %v, want [claude]", got)
    }
}

func TestActiveAgents_MultipleEnabled_Sorted(t *testing.T) {
    t.Parallel()
    cfg := &config.Config{
        Agents: config.AgentsConfig{
            Enabled: []string{"copilot", "claude"},
        },
    }
    got := cfg.ActiveAgents()
    if len(got) != 2 {
        t.Fatalf("ActiveAgents() = %v, want 2 entries", got)
    }
    if got[0] != "claude" || got[1] != "copilot" {
        t.Errorf("ActiveAgents() = %v, want [claude copilot] (sorted)", got)
    }
}

func TestActiveAgents_AllFourEnabled_Sorted(t *testing.T) {
    t.Parallel()
    cfg := &config.Config{
        Agents: config.AgentsConfig{
            Enabled: []string{"opencode", "copilot", "cursor", "claude"},
        },
    }
    got := cfg.ActiveAgents()
    want := []string{"claude", "copilot", "cursor", "opencode"}
    if len(got) != len(want) {
        t.Fatalf("ActiveAgents() = %v, want %d entries", got, len(want))
    }
    for i, w := range want {
        if got[i] != w {
            t.Errorf("ActiveAgents()[%d] = %q, want %q", i, got[i], w)
        }
    }
}

func TestActiveAgents_DedupesDuplicates(t *testing.T) {
    t.Parallel()
    cfg := &config.Config{
        Agents: config.AgentsConfig{
            Enabled: []string{"claude", "claude", "copilot"},
        },
    }
    got := cfg.ActiveAgents()
    if len(got) != 2 || got[0] != "claude" || got[1] != "copilot" {
        t.Errorf("ActiveAgents() = %v, want [claude copilot]", got)
    }
}
```

Update `TestAgentDefine_ApplyTo_MergesNonZeroFields` — remove `Enable` usage:

```go
func TestAgentDefine_ApplyTo_MergesNonZeroFields(t *testing.T) {
    t.Parallel()
    base := agent.Def{CLICommand: "claude", Description: "built-in"}
    override := config.AgentDefine{
        CLICommand: "claude-cli",
        LaunchArgs: "--version",
    }
    got := override.ApplyTo(base)
    if got.CLICommand != "claude-cli" {
        t.Errorf("CLICommand = %q, want claude-cli", got.CLICommand)
    }
    if got.LaunchArgs != "--version" {
        t.Errorf("LaunchArgs = %q, want --version", got.LaunchArgs)
    }
    if got.Description != "built-in" {
        t.Errorf("Description = %q, want built-in", got.Description)
    }
}
```

Add test rejecting `enable` in define:

```go
func TestLoad_RejectsEnableInAgentDefine(t *testing.T) {
    t.Parallel()
    dir := t.TempDir()
    path := filepath.Join(dir, "aivm.yaml")
    const content = `
agents:
  enabled:
    - claude
  define:
    claude:
      enable: true
`
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
    _, err := config.Load(path, config.Defaults{StateDir: dir})
    if err == nil {
        t.Fatal("expected error for enable field in define, got nil")
    }
    if !strings.Contains(err.Error(), "unknown field") {
        t.Fatalf("error = %q, want unknown field", err.Error())
    }
    if !strings.Contains(err.Error(), "enable") {
        t.Fatalf("error = %q, want enable mentioned", err.Error())
    }
}
```

Keep `TestLoad_RejectsUnknownAgentDefineField` but remove `enable: true` from
its YAML fixture:

```yaml
agents:
  enabled:
    - claude
  define:
    claude:
      launch_command: "nope"
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./test/unit/config/... \
  -run 'ActiveAgents|AgentDefine|RejectsEnable|RejectsUnknown' -v
```

Expected: FAIL — `AgentsConfig.Enabled` undefined or tests fail on old
behavior

- [ ] **Step 3: Implement `AgentsConfig.Enabled` and new `ActiveAgents()`**

In `internal/config/config.go`, add `Enabled` to `AgentsConfig`:

```go
type AgentsConfig struct {
    Default string `mapstructure:"default"`
    Enabled []string `mapstructure:"enabled"`
    Define  map[string]AgentDefine `mapstructure:"define"`
}
```

Delete the `activeNames()` method. Replace `ActiveAgents()`:

```go
// ActiveAgents returns the deduplicated, sorted names from agents.enabled.
func (c *Config) ActiveAgents() []string {
    seen := make(map[string]bool, len(c.Agents.Enabled))
    names := make([]string, 0, len(c.Agents.Enabled))
    for _, name := range c.Agents.Enabled {
        if name == "" || seen[name] {
            continue
        }
        seen[name] = true
        names = append(names, name)
    }
    sort.Strings(names)
    return names
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./test/unit/config/... \
  -run 'ActiveAgents|AgentDefine|RejectsEnable|RejectsUnknown' -v
```

Expected: PASS (except tests referencing `Enable` on `AgentDefine` —
fixed in Task 2)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go test/unit/config/agents_test.go
git commit -m "feat(config): read active agents from agents.enabled list"
```

---

### Task 2: Remove `Enable` from agent definitions

**Files:**

- Modify: `internal/agent/def.go`
- Modify: `internal/agent/defaults.yaml`

- [ ] **Step 1: Remove `Enable` from `Def` and `MergeDef`**

In `internal/agent/def.go`, delete the `Enable` field and its comment (lines
24–26). Remove the `Enable` block from `MergeDef` (lines 68–70).

- [ ] **Step 2: Remove `enable: false` from built-in defaults**

In `internal/agent/defaults.yaml`, delete the `enable: false` line from each
of the four agents (`claude`, `copilot`, `cursor`, `opencode`).

- [ ] **Step 3: Run agent and config unit tests**

Run: `go test ./test/unit/agent/... ./test/unit/config/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent/def.go internal/agent/defaults.yaml
git commit -m "refactor(agent): remove Enable field from agent definitions"
```

---

### Task 3: Update composition validation and error messages

**Files:**

- Modify: `internal/config/composition.go`
- Test: `test/unit/config/composition_test.go`

- [ ] **Step 1: Migrate composition test YAML fixtures**

Update every test YAML in `composition_test.go` from `define.enable` to
`agents.enabled`. Key replacements:

| Old test | New YAML snippet |
| --- | --- |
| `TestCompose_AllDefinedAgentsDisabled_Error` | Rename to `TestCompose_EmptyEnabledList_Error`; use `agents:\n  enabled: []` |
| `TestCompose_UnknownAgentInEnabledSet_Error` | `agents:\n  enabled:\n    - mystery` |
| `TestCompose_MultipleEnabled_NoDefault_Error` | `agents:\n  enabled:\n    - claude\n    - copilot` |
| `TestCompose_DefaultNotInEnabledSet_Error` | `agents:\n  default: copilot\n  enabled:\n    - claude` |
| `TestCompose_InvalidVMSessionEnvName_Error` | `agents:\n  enabled:\n    - claude` |
| `TestCompose_SingleEnabled_NoDefault_AutoInfersDefault` | `agents:\n  enabled:\n    - claude` |
| `TestCompose_MultipleEnabled_WithDefault_HappyPath` | `agents:\n  default: claude\n  enabled:\n    - claude\n    - copilot` |
| `TestCompose_DisabledAgentExcludedFromEnabledDefs` | Rename to `TestCompose_DefineWithoutEnabled_Excluded`; use `agents:\n  default: claude\n  enabled:\n    - claude\n  define:\n    opencode:\n      cli_command: opencode` — opencode must not appear in `EnabledAgentDefs` |
| `TestCompose_ActiveAgentDefMatchesDefault` | `agents:\n  default: copilot\n  enabled:\n    - copilot\n  define:\n    copilot:\n      cli_command: my-custom-copilot-cmd` |
| `TestCompose_UserAgentOverrideMerged` | `agents:\n  enabled:\n    - claude\n  define:\n    claude:\n      cli_command: claude-override\n      launch_args: --version` |

Update assertion in `TestCompose_DefaultNotInEnabledSet_Error`:

```go
if !strings.Contains(ce.Reason, "is not in agents.enabled") {
```

Update `TestCompose_ErrorMessage_IncludesExample` to check for `enabled:` in
the error message instead of only `agents:`.

- [ ] **Step 2: Run composition tests to verify failures**

Run: `go test ./test/unit/config/... -run Compose -v`

Expected: FAIL on old error message strings

- [ ] **Step 3: Update `composition.go` error messages and comments**

Replace the "no agents enabled" error (lines 92–98):

```go
Reason: "no agents enabled — add at least one agent to " +
    "agents.enabled in aivm.yaml\n" +
    "  Example:\n" +
    "    agents:\n" +
    "      enabled:\n" +
    "        - claude",
```

Replace unknown agent error (line 107):

```go
Reason: fmt.Sprintf(
    "unknown agent %q in agents.enabled — check your aivm.yaml",
    name,
),
```

Replace default-not-enabled error (line 136):

```go
Reason: fmt.Sprintf(
    "agents.default %q is not in agents.enabled",
    defaultAgentName,
),
```

Update struct comment on `EnabledAgentDefs` (line 62): "those listed in
agents.enabled".

- [ ] **Step 4: Run composition tests**

Run: `go test ./test/unit/config/... -run Compose -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/composition.go test/unit/config/composition_test.go
git commit -m "feat(config): update composition for agents.enabled"
```

---

### Task 4: Update test harness and remaining unit tests

**Files:**

- Modify: `test/framework/config.go`
- Modify: `test/unit/lifecycle/hash_test.go`
- Modify: `internal/lifecycle/service.go`

- [ ] **Step 1: Rewrite test harness agent config generation**

In `test/framework/config.go`, replace the agents block (lines 293–328) with:

```go
// agents
extraEnabled := make(map[string]bool, len(tc.ExtraEnabledAgents))
for _, name := range tc.ExtraEnabledAgents {
    extraEnabled[name] = true
}
preserveCLI := make(map[string]bool, len(tc.PreserveCLIAgents))
for _, name := range tc.PreserveCLIAgents {
    preserveCLI[name] = true
}

enabledAgents := []string{tc.Provider}
for _, name := range tc.ExtraEnabledAgents {
    if name != tc.Provider {
        enabledAgents = append(enabledAgents, name)
    }
}
sort.Strings(enabledAgents)

fmt.Fprintf(&sb, "agents:\n")
fmt.Fprintf(&sb, "  default: %q\n", tc.Provider)
fmt.Fprintf(&sb, "  enabled:\n")
for _, name := range enabledAgents {
    fmt.Fprintf(&sb, "    - %q\n", name)
}

var overrideAgents []string
for _, name := range []string{"claude", "copilot", "cursor", "opencode"} {
    if !extraEnabled[name] && name != tc.Provider {
        continue
    }
    if preserveCLI[name] {
        continue
    }
    cliCmd, launchArgs := agentLaunchFields(name, "")
    if tc.LaunchCommand != "" && name == tc.Provider {
        cliCmd, launchArgs = agentLaunchFields(name, tc.LaunchCommand)
    }
    if cliCmd == "" && launchArgs == "" {
        continue
    }
    overrideAgents = append(overrideAgents, name)
}
if len(overrideAgents) > 0 {
    sort.Strings(overrideAgents)
    fmt.Fprintf(&sb, "  define:\n")
    for _, name := range overrideAgents {
        fmt.Fprintf(&sb, "    %s:\n", name)
        cliCmd, launchArgs := agentLaunchFields(name, "")
        if tc.LaunchCommand != "" && name == tc.Provider {
            cliCmd, launchArgs = agentLaunchFields(name, tc.LaunchCommand)
        }
        if cliCmd != "" {
            fmt.Fprintf(&sb, "      cli_command: %q\n", cliCmd)
        }
        if launchArgs != "" {
            fmt.Fprintf(&sb, "      launch_args: %q\n", launchArgs)
        }
    }
}
```

Add `"sort"` to imports if not already present. Update the comment on
`ExtraEnabledAgents` (line 60): "additional agent names to include in
agents.enabled".

- [ ] **Step 2: Update hash_test.go inline YAML**

Replace all occurrences of:

```yaml
agents:
  default: claude
  define:
    claude:
      enable: true
```

With:

```yaml
agents:
  enabled:
    - claude
```

For the multi-agent hash test (line 184):

```yaml
agents:
  default: claude
  enabled:
    - claude
    - copilot
```

- [ ] **Step 3: Update lifecycle service comment**

In `internal/lifecycle/service.go` line 54, change comment to:
"agents listed in agents.enabled). Used by bootstrap to install all agents"

- [ ] **Step 4: Run unit tests**

Run: `go test ./test/unit/... -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add test/framework/config.go test/unit/lifecycle/hash_test.go \
  internal/lifecycle/service.go
git commit -m "test: migrate harness and hash tests to agents.enabled"
```

---

### Task 5: Update documentation and example configs

**Files:**

- Modify: `aivm.example.yaml`
- Modify: `README.md`
- Modify: `demo/configs/aivm.yaml`
- Modify: `test/e2e/multi_agent_test.go` (comments only)

- [ ] **Step 1: Update `aivm.example.yaml`**

Replace the agents section (lines 9–25) with:

```yaml
# To enable a single agent:
agents:
  enabled:
    - claude

# To enable multiple agents (all installed; choose default and override
# with --agent):
# agents:
#   enabled:
#     - claude
#     - copilot
#     - cursor
#   default: claude
```

Update the override comment (line 58) to remove "alongside enable: true":

```yaml
# (These can be added under agents.define.)
```

- [ ] **Step 2: Update README.md**

Key edits:

1. Quick Start (line 101): "Set `agents.enabled` and optionally
   `agents.default`"
2. Configuration snippet (lines 125–130):

   ```yaml
   agents:
     enabled:
       - claude
   ```

3. Agents section (lines 723–762): Replace references to `enable: true` with
   `agents.enabled`. Update the customizing-agents table — remove the `enable`
   row; add note that enablement is via `agents.enabled`:

   ```yaml
   agents:
     enabled:
       - claude
     define:
       claude:
         launch_args: "--dangerously-skip-permissions --model opus"
   ```

4. Per-agent example blocks (Claude, Copilot, Cursor, OpenCode): use
   `enabled: [agentname]` instead of `define.enable`.

5. Line 728: "All agents listed in `agents.enabled` are installed during
   bootstrap..."

6. Line 530 area if present: "Enabled agent plugins are registered
   automatically based on `agents.enabled`"

- [ ] **Step 3: Update `demo/configs/aivm.yaml`**

Replace:

```yaml
agents:
  default: opencode
  define:
    claude:
      enable: true
    copilot:
      enable: true
    opencode:
      enable: true
```

With:

```yaml
agents:
  default: opencode
  enabled:
    - claude
    - copilot
    - opencode
```

- [ ] **Step 4: Update e2e test comments**

In `test/e2e/multi_agent_test.go` lines 36–37, update to:

```go
//  1. Config: agents.default=claude, agents.enabled=[claude, opencode].
```

- [ ] **Step 5: Commit**

```bash
git add aivm.example.yaml README.md demo/configs/aivm.yaml \
  test/e2e/multi_agent_test.go
git commit -m "docs: migrate agent config examples to agents.enabled"
```

---

### Task 6: Full verification

**Files:** (none — verification only)

- [ ] **Step 1: Run full unit test suite**

Run: `go test ./test/unit/... -v`

Expected: PASS

- [ ] **Step 2: Run linter**

Run: `golangci-lint run ./...`

Expected: PASS (or only pre-existing issues)

- [ ] **Step 3: Build binary**

Run: `go build -o /tmp/aivm ./cmd/aivm`

Expected: success, no errors

- [ ] **Step 4: Smoke-test config loading**

Create a temp config and verify composition succeeds:

```bash
cat > /tmp/aivm-test.yaml <<'EOF'
agents:
  enabled:
    - claude
vm:
  cpus: 2
  memory: "4GB"
  disk: "20GB"
plugins:
  enabled:
    - system
EOF
go run ./cmd/aivm version  # sanity check binary runs
```

Manual: run `aivm` with migrated `~/.aivm/aivm.yaml` if available.

- [ ] **Step 5: Grep for stale references**

Run:

```bash
rg 'enable: true|agents\.define.*enable|def\.Enable' \
  --glob '*.{go,yaml,md}'
```

Expected: no matches in production code or docs (except `t3code.enable` and
`plugins.enabled`, which are unrelated)

- [ ] **Step 6: Final commit if any fixups needed**

```bash
git add -A
git commit -m "chore: fix remaining agents.enabled migration references"
```

---

## Spec coverage checklist

| Spec requirement | Task |
| --- | --- |
| `agents.enabled` list | Task 1 |
| Remove `enable` from defs | Task 2 |
| Dedupe duplicates | Task 1 |
| Updated error messages | Task 3 |
| `default` auto-infer (single) | Task 3 (existing logic, updated tests) |
| `define` for overrides only | Task 5 (docs); unchanged in code |
| Reject `enable` in `define` | Task 1 (`ValidateAgentsDefine` auto-rejects) |
| Harness/e2e test migration | Task 4, Task 5 |
| Docs/examples migration | Task 5 |
