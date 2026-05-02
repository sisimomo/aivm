# aivm

**aivm** is a CLI tool for macOS that runs [Claude Code](https://www.anthropic.com/claude-code) agents inside a secure, isolated [Colima](https://github.com/abiosoft/colima) VM. A single command handles the entire lifecycle — from first boot and toolchain installation to idle shutdown and VM teardown.

```
HOST (macOS)                            VM (Colima: aivm profile)
──────────────────────────────────────  ──────────────────────────────────────────
aivm CLI                                Docker Engine
MCPJungle (Docker Compose + SQLite)     Java · Maven · Node.js · Python
Idle Monitor daemon                     Claude Code
MCP Gateway  →  port 8080               MCP client → host.lima.internal:8080/mcp
```

---

## Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Plugin System](#plugin-system)
- [MCP / MCPJungle](#mcp--mcpjungle)
- [Idle Monitor](#idle-monitor)
- [Security Model](#security-model)
- [Architecture](#architecture)
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
# Builds bin/aivm, copies to /usr/local/bin/aivm, creates ~/.aivm/ state dirs,
# and creates aivm.yaml from aivm.example.yaml if it doesn't exist yet.

# 3. Set your Claude token
#    Open aivm.yaml and set auth.claude_token
#    (or export AIVM_AUTH_CLAUDE_TOKEN=<token> in your shell)

# 4. Launch from any project directory under ~/dev
cd ~/dev/my-project
aivm
```

`make install` does everything `install.sh` used to do — no separate script needed.

### Build from source only

```bash
make build        # produces bin/aivm
make vet          # go vet
make test         # go test ./...
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

Every key can also be overridden via environment variables using the `AIVM_` prefix and `_`-separated nesting:

```bash
export AIVM_AUTH_CLAUDE_TOKEN=sk-ant-...
export AIVM_VM_CPUS=8
export AIVM_IDLE_TIMEOUT=10m
```

### Full reference

```yaml
vm:
  cpus: 4
  memory: 8         # GiB
  disk: 60          # GiB
  type: vz          # vz (Apple Silicon, macOS 13+) | qemu (Intel/fallback)
  max_age_days: 7   # days before prompting to recreate the VM
  dev_root: ~/dev   # host directory mounted in VM at the same absolute path
  profile: aivm     # Colima profile name

mcp:
  port: 8080
  data_dir: ~/.aivm/mcpjungle-data
  image_tag: latest-stdio

idle:
  timeout: 5m       # auto-shutdown after this idle duration (e.g. 5m, 300s)

auth:
  claude_token: ""  # Claude Code OAuth token — prefer AIVM_AUTH_CLAUDE_TOKEN env

plugins:
  enabled:
    - system
    - java
    - maven
    - nodejs
    - python
    - rtk
    - claude

  config:
    java:
      version: "25"
      distribution: temurin
    nodejs:
      version: lts

  # Custom script plugins — see Plugin System section below
  # custom:
  #   - name: my-tool
  #     script: ~/.aivm/plugins/my-tool.sh
  #     dependencies: [python]
  #     config:
  #       version: "1.0.0"
```

---

## Usage

```bash
# Launch Claude Code in the current directory
# (starts the VM and services if not already running)
aivm
aivm /path/to/project     # explicit path

# VM lifecycle
aivm start                # start VM + MCPJungle (no Claude session)
aivm stop                 # stop VM + MCPJungle (disk preserved)
aivm restart              # stop then start
aivm destroy              # delete VM entirely (host state preserved)

# Status and diagnostics
aivm status               # VM state, sessions, idle countdown
aivm ssh                  # interactive shell inside the VM

# Bootstrap
aivm bootstrap            # re-run the full bootstrap
aivm bootstrap --list     # list all plugins and their status
aivm bootstrap --plugin java   # run only the java plugin

# Logs
aivm logs mcpjungle       # MCPJungle container logs (live)
aivm logs monitor         # idle monitor daemon log
aivm logs bootstrap       # bootstrap log from inside VM
aivm logs colima          # Colima VM log

aivm version
aivm help
```

### Path mapping

`aivm` runs Claude Code in your current directory inside the VM. Colima mounts `~/dev` at the **same absolute path**, so no translation is needed:

| Host | VM |
|---|---|
| `/Users/you/dev/my-project` | `/Users/you/dev/my-project` |

You must run `aivm` from within your `vm.dev_root` (default `~/dev`). Paths outside it will produce a clear error.

### Multiple sessions

Multiple `aivm` invocations can run simultaneously in the same VM — each gets its own Claude Code process and session lock file. The idle monitor only shuts down the VM when *all* sessions have ended.

---

## Plugin System

The bootstrap process that provisions the VM is powered by a **plugin system** with dependency ordering. Each plugin declares the tools it installs and which other plugins must run before it. The engine resolves the full dependency graph (DAG) and executes plugins in the correct order.

### Built-in plugins

| Plugin | Installs | Depends on |
|---|---|---|
| `system` | `git`, `curl`, `jq`, apt packages, PATH profile | — |
| `java` | Temurin JDK (default: 25) via Adoptium repo | `system` |
| `maven` | Apache Maven latest 3.x | `java` |
| `nodejs` | Node.js LTS via NodeSource | `system` |
| `python` | Python via `uv` | `system` |
| `rtk` | rtk CLI | `system` |
| `claude` | Claude Code CLI, MCP config, rtk init | `nodejs`, `rtk` |

Enable or disable plugins in `aivm.yaml` under `plugins.enabled`. The engine automatically handles ordering — you don't need to list them in dependency order.

Each plugin is **idempotent**: it runs `Check()` first and skips `Install()` if the tool is already present.

### Custom plugins (no Go required)

You can add your own plugins as shell scripts — no forking or modifying the core repo needed.

**1. Write a script** (e.g. `~/.aivm/plugins/rust.sh`):

```bash
#!/usr/bin/env bash
# AIVM_PLUGIN_CONFIG is set to a JSON object with your plugin's config block.
# The script runs inside the VM during bootstrap.

set -euo pipefail

VERSION=$(echo "$AIVM_PLUGIN_CONFIG" | python3 -c "import sys,json; print(json.load(sys.stdin).get('version','stable'))")

curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain "$VERSION"
source "$HOME/.cargo/env"
rustc --version
```

**2. Register it in `aivm.yaml`:**

```yaml
plugins:
  enabled:
    - system
    - java
    - maven
    - nodejs
    - python
    - rtk
    - claude
    - rust           # ← add your plugin name here

  custom:
    - name: rust
      description: "Rust toolchain via rustup"
      script: ~/.aivm/plugins/rust.sh
      dependencies: [system]   # runs after 'system'
      config:
        version: stable
```

**3. Run bootstrap:**

```bash
aivm bootstrap --plugin rust   # run only this plugin
# or
aivm bootstrap                 # re-run all (skips already-installed)
```

The script receives `AIVM_PLUGIN_CONFIG` as a JSON string containing whatever you put under `config:` in `aivm.yaml`. The script runs **inside the Colima VM**, so you can freely install system packages, compile tools, etc.

### Writing a Go plugin (built-in)

If you want a compiled plugin bundled in the binary, implement the `Plugin` interface and call `Register()` in `init()`:

```go
package myplugin

import (
    "context"
    "aivm/internal/plugin"
)

func init() { plugin.Register(&MyPlugin{}) }

type MyPlugin struct{}

func (p *MyPlugin) Name()         string   { return "myplugin" }
func (p *MyPlugin) Description()  string   { return "My custom tool" }
func (p *MyPlugin) Dependencies() []string { return []string{"system"} }

func (p *MyPlugin) Check(ctx context.Context, env plugin.InstallEnv) (bool, error) {
    err := env.VM.Run(ctx, "which mytool >/dev/null 2>&1", nil)
    return err == nil, nil
}

func (p *MyPlugin) Install(ctx context.Context, env plugin.InstallEnv) error {
    version := env.ConfigString("version", "latest")
    return env.VM.Run(ctx, "curl ... | install-mytool "+version, nil)
}

func (p *MyPlugin) Configure(ctx context.Context, env plugin.InstallEnv) error {
    return nil
}
```

Then add a blank import in `plugins/register.go`:

```go
import _ "aivm/plugins/myplugin"
```

---

## MCP / MCPJungle

[MCPJungle](https://github.com/mcpjungle/mcpjungle) is an MCP gateway that runs as a Docker container on the host. All Claude Code sessions inside the VM connect to it via `host.lima.internal:8080/mcp`.

```
Claude Code (VM)
    │  http://host.lima.internal:8080/mcp
    ▼
MCPJungle Gateway  (host Docker, port 8080)
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

MCP registrations persist in SQLite — you only need to register once.

---

## Idle Monitor

A background daemon (`aivm __monitor`) watches for active sessions and automatically shuts down the VM when idle.

- Polls every 30 seconds for session lock files
- A session is alive if its PID responds to `signal(0)` — stale locks from killed terminals are cleaned up automatically
- After `idle.timeout` (default `5m`) with no active sessions:
  1. Stops Docker containers inside the VM
  2. Stops the Colima VM (disk preserved)
  3. Stops MCPJungle

The daemon spawns itself as a detached process when `aivm start` or `aivm` is run. It exits automatically once the shutdown is complete. Check its log with `aivm logs monitor`.

---

## Security Model

| Concern | Decision |
|---|---|
| SSH keys | **None** inside the VM |
| Git credentials | **None** inside the VM |
| Claude token | Stays on host (`aivm.yaml` or env var), injected at launch only |
| MCPJungle | Binds to `127.0.0.1` only — not reachable outside the host |
| VM isolation | Colima VM has no inbound network exposure |
| Disposal | `aivm destroy` wipes the VM; `aivm` rebuilds from scratch automatically |
| State persistence | MCPJungle data at `~/.aivm/mcpjungle-data/` — survives VM deletion |

The VM is designed to be **disposable by default**. When it exceeds `vm.max_age_days` (default: 7), `aivm start` offers to recreate it interactively.

---

## Architecture

```
aivm/
├── cmd/aivm/main.go              ← CLI entrypoint (Cobra)
├── internal/
│   ├── config/config.go          ← Config struct, Viper loader, env overrides
│   ├── plugin/
│   │   ├── plugin.go             ← Plugin interface + InstallEnv
│   │   ├── registry.go           ← Global plugin registry
│   │   ├── dag.go                ← Topological sort (Kahn's algorithm)
│   │   ├── executor.go           ← DAG-ordered install runner
│   │   └── script.go             ← Shell script plugin adapter
│   ├── vm/
│   │   ├── vm.go                 ← VM interface
│   │   ├── colima.go             ← Colima implementation
│   │   ├── lifecycle.go          ← Atomic lifecycle lock
│   │   └── image.go              ← Snapshot / image management
│   ├── bootstrap/bootstrap.go    ← Bootstrap engine (runs plugins in VM)
│   ├── monitor/idle.go           ← Idle monitor daemon
│   ├── mcp/
│   │   ├── docker.go             ← Host Docker socket detection
│   │   └── compose.go            ← MCPJungle lifecycle
│   ├── session/session.go        ← Session lock files + liveness check
│   ├── cli/                      ← One file per command
│   ├── log/log.go                ← Colored structured logger
│   └── run/run.go                ← exec.Command helpers
├── plugins/
│   ├── register.go               ← Blank-imports all built-in plugins
│   ├── system/   java/   maven/
│   ├── nodejs/   python/ rtk/
│   └── claude/
├── config/
│   ├── colima.yaml               ← Colima VM config reference
│   └── mcp-client-config.json   ← MCP client config template
├── aivm.example.yaml             ← Config template (copy to aivm.yaml)
└── Makefile
```

---

## Troubleshooting

**`No suitable host Docker runtime found`**
> MCPJungle needs a Docker runtime separate from the aivm VM. Install Docker Desktop, OrbStack, or run `colima start` (default profile).

**Claude Code can't reach MCPJungle**
> From inside the VM (`aivm ssh`), run: `curl http://host.lima.internal:8080/health`
> If that fails, check host-side status: `aivm status` and `aivm logs mcpjungle`.

**Bootstrap fails for a plugin**
> Run `aivm bootstrap --plugin <name>` to retry just that plugin. Add `--debug` for verbose output.
> Open a shell with `aivm ssh` to inspect the VM directly.

**Idle monitor doesn't shut down the VM**
> Check `aivm logs monitor`. Ensure no stale lock files remain: `ls ~/.aivm/sessions/`.

**VM is slow or running out of disk**
> Increase resources in `aivm.yaml` (`vm.cpus`, `vm.memory`, `vm.disk`), then `aivm destroy && aivm start` to apply.

**Reset everything**

```bash
aivm destroy                 # delete VM (host state preserved)
rm -rf ~/.aivm/sessions/     # clear stale session locks if any
aivm                         # fresh VM, bootstrap runs automatically
```
