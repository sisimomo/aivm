# Context7 Plugin Design

## Summary

Add a `context7` plugin to aivm that installs the Context7 CLI and the `find-docs` agent skill across all supported AI agents. MCP mode is out of scope — skills-only via `npx skills@latest`.

## Goals

- Install `ctx7` globally so agents can run `ctx7 library` and `ctx7 docs`
- Install the `find-docs` skill to every agent directory via `npx skills@latest add upstash/context7 --global --yes --agent '*' --skill find-docs`
- Idempotent bootstrap (`skip_if` detects existing install)

## Non-Goals

- MCP server wiring (`ctx7 setup --mcp`)
- Agent rules (`context7.md`, AGENTS.md sections) — the skill alone is sufficient for CLI mode
- Per-agent targeting — always install to all agents (`--agent '*'`)
- Plugin-level API key configuration — use `vm.env` instead (see Configuration)

## Plugin Definition

**Name:** `context7`

**Dependencies:** `mise-node` (provides `npm` and `npx`)

**Setup steps:**

1. `npm install -g ctx7@latest`
2. `npx skills@latest add upstash/context7 --global --yes --agent '*' --skill find-docs`

**skip_if:**

```bash
command -v ctx7 >/dev/null 2>&1 && \
find "$HOME" -path "*/skills/find-docs/SKILL.md" 2>/dev/null | grep -q .
```

## Configuration

Enable the plugin:

```yaml
plugins:
  enabled:
    - context7
```

No API key is required — `ctx7 library` and `ctx7 docs` work without authentication. For higher rate limits, set `CONTEXT7_API_KEY` via `vm.env` (supports `${HOST_VAR}` expansion from the host shell):

```yaml
plugins:
  enabled:
    - context7

vm:
  env:
    CONTEXT7_API_KEY: "${CONTEXT7_API_KEY}"
```

This follows the same pattern as other secrets in aivm: the value is injected into every VM session, applied in-place on the next `aivm` run (no VM recreation needed), and can reference host environment variables.

## Agent Coverage

`npx skills@latest --agent '*'` installs into every agent directory the skills CLI supports — Claude Code, Cursor, GitHub Copilot, OpenCode, and others. This matches the cocoindex-code pattern (`--global --all`) but scoped to the `find-docs` skill.

## Testing

Bootstrap tests (Docker, `//go:build bootstrap`):

- `ctx7` is on PATH and runnable
- `find-docs/SKILL.md` exists under `$HOME`
- `skip_if` passes on re-run

## Documentation

- Add `context7` to the plugins table in README
- Add a **Context7** section with enable instructions and optional `CONTEXT7_API_KEY` via `vm.env`
- Add commented example in `aivm.example.yaml`
