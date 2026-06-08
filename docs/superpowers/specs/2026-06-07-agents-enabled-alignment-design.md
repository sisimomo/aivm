# Agents Enabled List Design

## Summary

Align agent enablement with the plugin model by introducing `agents.enabled`
as an explicit list. Remove per-agent `enable: true` from `agents.define`.
The `define` section remains for overriding built-in agents or defining custom
ones, but is omitted from basic examples because built-ins need no overrides.

## Goals

- **Consistency** — same mental model as `plugins.enabled`: list what you want
- **UX** — simpler config for the common case (one or more agent names, no
  nested `enable` flags)
- **Alpha hard cutover** — no backward compatibility, deprecation warnings, or
  dual-path logic for the old `enable` field

## Non-Goals

- Unifying agents into the plugins config section
- Changing bootstrap mechanics (agents still merge into the plugin registry)
- Changing `--agent` flag behavior or T3 Code integration
- Adding `aivm agents list` CLI (can be a follow-up)

## Config Schema

### Typical config (shown in examples)

```yaml
agents:
  enabled:
    - claude
```

### Multi-agent

```yaml
agents:
  enabled:
    - claude
    - copilot
  default: claude
```

`agents.default` is auto-inferred when exactly one agent is in `enabled`.
When two or more are enabled, `default` is required.

### Optional overrides (supported, not shown in examples)

```yaml
agents:
  enabled:
    - claude
  define:
    claude:
      launch_args: "--dangerously-skip-permissions"
    my-agent:
      cli_command: my-agent-cli
      setup: |
        curl -fsSL https://example.com/install | sh
```

`define` is override-only. Names listed under `define` but not in `enabled`
are ignored (same semantics as `plugins.define`).

## Validation Rules

| Rule | Behavior |
| --- | --- |
| `agents.enabled` empty | Error: no agents enabled |
| Unknown name in `enabled` | Error: not a registered built-in agent (unless custom via `define`) |
| Custom agent in `enabled` without `cli_command` | Error: must set `cli_command` in `agents.define` |
| `default` missing, 1 enabled | Auto-set to that agent |
| `default` missing, 2+ enabled | Error: must set `agents.default` |
| `default` not in `enabled` | Error |
| `enable` under `define` | Rejected as unknown field (hard cutover) |
| Duplicate names in `enabled` | Dedupe silently |

## Error Messages

| Situation | Message |
| --- | --- |
| Empty `agents.enabled` | `no agents enabled — add at least one agent to agents.enabled` |
| Unknown name in `enabled` | `unknown agent "foo" in agents.enabled` |
| Multiple enabled, no `default` | `agents.default must be set when multiple agents are enabled` |
| `default` not in `enabled` | `agents.default "copilot" is not in agents.enabled` |
| Custom agent missing `cli_command` | `agent "my-agent": cli_command must be set in agents.define` |

## Code Changes

### `internal/config/config.go`

- Add `Enabled []string` to `AgentsConfig`
- Change `ActiveAgents()` to return sorted `Agents.Enabled`

### `internal/agent/def.go`

- Remove `Enable` field from `Def`
- Remove `Enable` handling from `MergeDef`

### `internal/agent/defaults.yaml`

- Remove all `enable: false` lines (enablement is user-config only)

### `internal/config/composition.go`

- Read enabled agents from `cfg.ActiveAgents()` (backed by `agents.enabled`)
- Update all error messages to reference `agents.enabled` instead of
  `agents.define` + `enable: true`
- Keep existing merge flow: built-in defs → user `define` overrides → filter
  to names in `enabled`

### Unchanged

- Agent defs still convert to plugin defs and merge into the plugin registry
  for VM bootstrap
- `BootstrapEnabledPlugins` still auto-adds agent dependency plugins
- `ValidateAgentsDefine` still validates override keys (minus `enable`)
- `--agent` flag and session launch behavior

## Documentation & Test Updates

- `aivm.example.yaml` — use `agents.enabled`; remove `enable: true` examples
- `README.md` — update Configuration, Quick Start, and Agents sections
- `demo/configs/aivm.yaml` — migrate to `agents.enabled`
- `test/framework/config.go` — generate `enabled:` instead of `enable: true`
- Unit tests: `test/unit/config/agents_test.go`,
  `test/unit/config/composition_test.go`
- E2E tests: `test/e2e/multi_agent_test.go` and any others referencing
  `enable: true`

## Testing

Unit tests:

- `ActiveAgents()` reads from `agents.enabled` (empty, single, multiple,
  sorted)
- Composition rejects empty `enabled`, unknown names, missing `default` with
  multiple agents, `default` not in `enabled`
- Composition auto-infers `default` with single enabled agent
- `enable` field in `define` rejected as unknown
- Overrides via `define` still merge correctly for enabled agents
- Custom agent in `enabled` + `define` with `cli_command` works

Existing bootstrap and e2e tests updated to new config shape; no new
bootstrap scenarios required (behavior unchanged, config surface only).
