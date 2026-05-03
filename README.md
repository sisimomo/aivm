# aivm

**aivm** is a CLI tool for macOS that launches AI coding agents (Claude Code or GitHub Copilot) inside a secure, isolated [Colima](https://github.com/abiosoft/colima) VM. A single command handles the entire lifecycle — from first boot and toolchain installation to idle suspension and VM teardown.

```
HOST (macOS)                            VM (Colima: aivm profile)
──────────────────────────────────────  ──────────────────────────────────────────
aivm CLI                                Java · Maven · Node.js · Python · Go
MCPJungle (Docker + SQLite)             Claude Code  ─or─  GitHub Copilot
Idle Monitor daemon                     rtk (token optimizer)
MCP Gateway  →  port 7593               MCP client → host.lima.internal:7593/mcp
```

---

## Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Concepts: Plugins, Agents, Integrations](#concepts-plugins-agents-integrations)
- [Choosing an Agent](#choosing-an-agent)
- [Plugin System](#plugin-system)
- [Integration System](#integration-system)
- [MCP / MCPJungle](#mcp--mcpjungle)
- [Idle Monitor & VM Lifecycle](#idle-monitor--vm-lifecycle)
- [Base Image System](#base-image-system)
- [Security Model](#security-model)
- [Troubleshooting](#troubleshooting)

---

## Requirements

| Tool | Purpose | Install |
|---|---|---|
| [Colima](https://github.com/abiosoft/colima) | VM runtime | `brew install colima` |
| Docker runtime | MCPJungle host container | Docker Desktop, OrbStack, or `colima start` (default profile) |

> **Docker note:** MCPJungle runs as a Docker container *on the host*, separate from the aivm VM. If Colima is your only Docker runtime, run `colima start` (the default profile) once before using `aivm`.

**Platform:** macOS only. Apple Silicon (`vz` VM type) or Intel (`qemu`).

---

## Installation

```bash
# 1. Clone the repository
git clone <repo-url> ~/dev/aivm
cd ~/dev/aivm

# 2. Build and install the binary
make install
# Builds bin/aivm, copies to /usr/local/bin/aivm, and creates ~/.aivm/ state dirs.
# Creates aivm.yaml from aivm.example.yaml if it doesn't exist yet.

# 3. Launch from any project directory under ~/dev
cd ~/dev/my-project
aivm
```

On first launch, aivm bootstraps the VM (installs tools) and then opens an agent session. Subsequent launches are fast — the VM restores from a base image snapshot and skips full bootstrap.

### Build targets

```bash
make build        # produces bin/aivm
make build-dev    # produces bin/aivm-dev (isolated state in ~/.aivm-dev)
make install-dev  # install the dev build alongside the production one
make test         # go test ./...
make vet          # go vet
```

---

## Configuration

Configuration lives in `aivm.yaml`. Copy the example and edit:

```bash
cp aivm.example.yaml aivm.yaml
```

`aivm` searches for `aivm.yaml` in this order:
1. Path from `--config` flag
2. Current working directory
3. `$AIVM_REPO_ROOT` environment variable
4. `~/.aivm/aivm.yaml`

Every key can be overridden via environment variables using the `AIVM_` prefix with `_`-separated nesting:

```bash
export AIVM_VM_CPUS=8
export AIVM_IDLE_TIMEOUT=10m
export AIVM_AGENTS_ENABLED=copilot
```

### Full reference

```yaml
vm:
  cpus: 4
  memory: 8                    # GiB
  disk: 60                     # GiB
  type: vz                     # vz (Apple Silicon, macOS 13+) | qemu (Intel/fallback)
  max_age_days: 7              # days before prompting to recreate the VM
  base_image_max_age_days: 7   # days before prompting to rebuild the base image
  dev_root: ~/dev              # host directory mounted in VM at the same absolute path
  profile: aivm                # Colima profile name

mcp:
  port: 7593
  data_dir: ~/.aivm/mcpjungle-data
  image_tag: latest-stdio

idle:
  timeout: 5m        # suspend VM after this idle duration (Phase 1)
  delete_timeout: 5m # delete suspended VM after this additional duration (Phase 2)

agents:
  enabled: claude    # claude | copilot  (exactly one agent at a time)

  # Optional per-agent configuration:
  # config:
  #   copilot:
  #     launch_command: "gh copilot"

plugins:
  enabled:
    - system
    - java
    - maven
    - nodejs
    - python
    - golang
    - rtk
    # The active agent plugin (claude or copilot) is added automatically.
    # You do not need to list it here.

  config:
    java:
      version: "25"
      distribution: temurin
    nodejs:
      version: lts
    python:
      tool: uv

  # Define custom or override built-in plugins (YAML, no Go required):
  # define:
  #   rust:
  #     description: "Rust toolchain via rustup"
  #     dependencies: [system]
  #     check: |
  #       rustc --version >/dev/null 2>&1
  #     install: |
  #       curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
```

---

## Usage

```bash
# Launch the configured AI agent in the current directory
# (starts the VM and services if not already running)
aivm
aivm /path/to/project     # explicit path

# VM lifecycle
aivm start                # start VM + MCPJungle (no agent session)
aivm stop                 # suspend VM + stop MCPJungle (disk preserved)
aivm restart              # stop then start
aivm destroy              # delete VM entirely (host state preserved)

# Status and diagnostics
aivm status               # VM state, base image info, sessions, idle countdown
aivm ssh                  # interactive shell inside the VM

# Bootstrap
aivm bootstrap            # install any missing tools
aivm bootstrap --force    # re-run all plugins unconditionally
aivm bootstrap --list     # list all plugins and their status
aivm bootstrap --plugin java   # run only the java plugin

# Base image
aivm rebuild-image        # rebuild the base image from scratch

# Logs
aivm logs mcpjungle       # MCPJungle container logs (live)
aivm logs monitor         # idle monitor daemon log
aivm logs bootstrap       # bootstrap log from inside VM
aivm logs colima          # Colima VM log

aivm version
aivm help
```

### Path mapping

`aivm` runs the agent in your current directory inside the VM. Colima mounts `~/dev` at the **same absolute path**, so no translation is needed:

| Host | VM |
|---|---|
| `/Users/you/dev/my-project` | `/Users/you/dev/my-project` |

You must run `aivm` from within `vm.dev_root` (default `~/dev`). Paths outside it produce a clear error.

### Multiple sessions

Multiple `aivm` invocations can run simultaneously in the same VM — each gets its own agent process and session lock file. The idle monitor only suspends the VM when *all* sessions have ended.

---

## Concepts: Plugins, Agents, Integrations

aivm is built around three cleanly separated concepts:

### Plugins — capabilities

A **plugin** installs a tool or configures the VM environment. Plugins are independent of agents — they know nothing about Claude, Copilot, or any other AI provider. Each plugin declares:

- A `check` script (idempotency: skip if already installed)
- An `install` script (run once)
- Optional `dependencies` (resolved automatically)
- Optional `configure` script (post-install)

Examples: `java`, `nodejs`, `rtk`, `claude`, `copilot`

### Agents — runtime identities

An **agent** is an AI coding assistant (Claude Code, GitHub Copilot, etc.). Agents are fully decoupled from plugins. You configure the active agent via `agent.provider`. Each provider:

- Identifies itself by name (`claude`, `copilot`)
- Declares its required VM plugin (e.g. the `claude` plugin)
- Handles its own launch command

Agents have no knowledge of which plugins are installed.

### Integrations — cross-cutting configuration

An **integration** bridges a plugin and an agent. It runs a `configure` script when:

1. The specified plugin is **installed**
2. The specified agent is **active**
3. The `when` condition is satisfied (currently `installed`)

Integrations are the *only* mechanism for agent-specific plugin configuration. Plugins never contain agent-specific logic; agents never contain plugin-specific logic.

Built-in example: when `rtk` is installed *and* `claude` (or `copilot`) is active, the rtk→claude (or rtk→copilot) integration runs `rtk init -g --auto-patch` to wire rtk into the agent's global config.

### Execution model

```
1. Install plugins   — resolve dependency graph, run check+install for each
2. Initialize agents — select active provider from agent.provider
3. Resolve integrations — find matching (installed plugin × active agent) pairs
4. Execute integrations — run configure scripts for each match
```

Integrations are **idempotent**: each `from:to` pair runs exactly once, tracked in bootstrap state.

---

## Choosing an Agent

Set `agent.provider` in `aivm.yaml` (or `AIVM_AGENT_PROVIDER` env var):

| Provider | Value | Authentication |
|---|---|---|
| [Claude Code](https://www.anthropic.com/claude-code) | `claude` | Run `claude auth` inside the VM (`aivm ssh`) |
| [GitHub Copilot](https://github.com/features/copilot) | `copilot` | Run `gh auth login` inside the VM (`aivm ssh`) |

Switching providers is enough — aivm automatically adds the right bootstrap plugin (`claude` or `copilot`) based on `agent.provider`. You do not need to edit the `plugins.enabled` list.

---

## Plugin System

When a VM is first created, aivm runs a **bootstrap** process that installs all required tools using a plugin system. Each plugin declares what it installs and which plugins must run before it. The engine resolves the full dependency graph and executes plugins in order.

All plugins are defined in YAML — no Go code required.

### Built-in plugins

| Plugin | Installs | Depends on |
|---|---|---|
| `system` | `git`, `curl`, `jq`, common apt packages, PATH setup | — |
| `java` | Temurin JDK (default: 25) via Adoptium | `system` |
| `maven` | Apache Maven (latest 3.x) | `java` |
| `nodejs` | Node.js LTS via NodeSource | `system` |
| `python` | Python via `uv` | `system` |
| `golang` | Go via apt | `system` |
| `rtk` | rtk (token optimizer CLI) | `system` |
| `claude` | Claude Code CLI + MCP config | `nodejs` |
| `copilot` | GitHub CLI + Copilot extension + MCP config | `system` |

Enable or disable plugins under `plugins.enabled`. Dependency order is handled automatically.

Each plugin is **idempotent**: it checks whether the tool is already installed before running.

> **Note:** Agent-specific setup (e.g. wiring rtk into Claude's config) is handled by [integrations](#integration-system), not by the plugin itself.

### Custom plugins

Add your own plugins directly in `aivm.yaml` — no forking needed:

```yaml
plugins:
  enabled:
    - system
    - nodejs
    - rust          # ← add your plugin name here

  define:
    rust:
      description: "Rust toolchain via rustup"
      dependencies: [system]
      check: |
        rustc --version >/dev/null 2>&1
      install: |
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
        source "$HOME/.cargo/env"
        rustc --version
```

Then run:

```bash
aivm bootstrap --plugin rust   # install only this plugin
# or
aivm bootstrap                 # install all missing plugins
```

Plugin scripts run **inside the Colima VM**, so you can freely install system packages, compile from source, etc.

You can also use Go template syntax in scripts. Values from `plugins.config.<name>` are available as template variables (e.g. `{{ .version }}`).

---

## Integration System

Integrations apply cross-cutting configuration when a plugin is installed *and* an agent is active. They run after all plugins are installed.

### Built-in integrations

| Integration | Runs when | Effect |
|---|---|---|
| `rtk → claude` | rtk installed + claude active | `rtk init -g --auto-patch` |
| `rtk → copilot` | rtk installed + copilot active | `rtk init -g --auto-patch` |

### Custom integrations

Add your own in `aivm.yaml`:

```yaml
integrations:
  - from: rtk
    to: claude
    when: installed
    configure: |
      rtk init -g --auto-patch 2>/dev/null || true
  - from: rtk
    to: copilot
    when: installed
    configure: |
      rtk init -g --auto-patch 2>/dev/null || true
```

- `from`: plugin name that must be installed
- `to`: agent name that must be active
- `when`: condition — currently only `installed` is supported
- `configure`: shell script run inside the VM when the condition is satisfied

Integrations are **idempotent**: each `from:to` pair is tracked in bootstrap state and runs exactly once.

---

## MCP / MCPJungle

[MCPJungle](https://github.com/mcpjungle/mcpjungle) is an MCP gateway that runs as a Docker container on the host. All agent sessions inside the VM connect to it via `host.lima.internal:7593/mcp`.

```
Agent (VM)
    │  http://host.lima.internal:7593/mcp
    ▼
MCPJungle Gateway  (host Docker, port 7593)
    │  SQLite at ~/.aivm/mcpjungle-data/
    ├── Registered MCP server A
    ├── Registered MCP server B
    └── ...
```

MCPJungle starts automatically with `aivm start` and stops when the VM goes idle.

**Register an MCP server** (from the host, after `aivm start`):

```bash
mcpjungle register \
  --name context7 \
  --url https://mcp.context7.com/mcp \
  --description "Context7 documentation"
```

MCP registrations persist in SQLite — you only need to register once. They survive VM deletion.

---

## Idle Monitor & VM Lifecycle

A background daemon watches for active sessions and automatically manages the VM when idle. It operates in two phases:

**Phase 1 — Suspend** (after `idle.timeout`, default `5m` of no active sessions):
- Suspends the Colima VM (disk preserved)
- Stops MCPJungle

**Phase 2 — Delete** (after `idle.delete_timeout`, default `5m` of suspension):
- Deletes the VM entirely to reclaim disk and memory
- On next `aivm` or `aivm start`, the VM is recreated from the base image snapshot (fast) or re-bootstrapped from scratch

Running `aivm start` or `aivm` at any point cancels the pending deletion.

The daemon starts automatically and exits when it has nothing left to monitor. Check its log with `aivm logs monitor`.

---

## Base Image System

To make VM recreation fast, aivm maintains a **base image** — a snapshot of the VM taken after a successful bootstrap.

- On first boot: full bootstrap runs, then a snapshot is saved as the base image
- On subsequent VM creations (after idle deletion or `vm.max_age_days` rotation): the base image is restored in seconds, skipping bootstrap entirely
- If any plugins have changed since the base image was taken, `syncBootstrap` installs only the missing ones

When the base image is older than `vm.base_image_max_age_days` (default: 7), aivm prompts you to rebuild it on the next session launch.

**Rebuild manually:**

```bash
aivm rebuild-image
```

If you have active sessions, you can choose:
- **Hard rebuild**: kill all sessions, destroy and recreate the VM now
- **Soft rebuild**: bootstrap a temporary VM in parallel; the current VM keeps running until all its sessions end, then auto-deletes

---

## Security Model

| Concern | Decision |
|---|---|
| SSH keys | **None** inside the VM |
| Git credentials | **None** inside the VM |
| Agent credentials | Managed externally via `claude auth` or `gh auth login` inside the VM |
| MCPJungle | Binds to `127.0.0.1` only — not reachable outside the host |
| VM isolation | Colima VM has no inbound network exposure |
| Disposal | `aivm destroy` wipes the VM; the next `aivm` rebuilds from the base image |
| State persistence | MCPJungle data and Claude projects at `~/.aivm/` — survive VM deletion |

The VM is designed to be **disposable**. When it exceeds `vm.max_age_days` (default: 7), `aivm start` offers to recreate it interactively.

---

## Troubleshooting

**`No suitable host Docker runtime found`**
> MCPJungle needs a Docker runtime on the host. Install Docker Desktop, OrbStack, or run `colima start` (the default profile).

**Agent can't reach MCPJungle**
> From inside the VM (`aivm ssh`), run: `curl http://host.lima.internal:7593/health`
> If that fails, check: `aivm status` and `aivm logs mcpjungle`.

**Bootstrap fails for a plugin**
> Run `aivm bootstrap --plugin <name>` to retry just that plugin. Add `--debug` for verbose output.
> Open a shell with `aivm ssh` to inspect the VM directly.

**Idle monitor doesn't suspend the VM**
> Check `aivm logs monitor`. Ensure no stale lock files remain: `ls ~/.aivm/sessions/`.

**VM is slow or running out of disk**
> Increase resources in `aivm.yaml` (`vm.cpus`, `vm.memory`, `vm.disk`), then `aivm destroy && aivm start` to apply.

**Reset everything**

```bash
aivm destroy                 # delete VM (host state preserved)
rm -rf ~/.aivm/sessions/     # clear stale session locks if any
aivm                         # fresh VM, restores from base image or bootstraps
```
