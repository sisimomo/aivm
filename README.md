# aivm

> Launch AI agents in a secure, disposable [Colima](https://github.com/abiosoft/colima) VM — with one command.

[![CI](https://github.com/sisimomo/aivm/actions/workflows/ci.yml/badge.svg)](https://github.com/sisimomo/aivm/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](go.mod)
[![macOS only](https://img.shields.io/badge/platform-macOS-lightgrey.svg)](#requirements)

**aivm** manages the full lifecycle of a Colima Linux VM dedicated to running AI coding agents. It bootstraps the VM with your choice of toolchain plugins, wires up [MCP](https://modelcontextprotocol.io/) via [mcpjungle](https://github.com/HenrySchulz/mcpjungle), keeps sessions alive while you work, and auto-cleans up when you're idle.

Supported agents: **Claude Code** (`claude`) · **GitHub Copilot** (`copilot`)

---

## Table of Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
  - [VM Resources](#vm-resources)
  - [Mounts](#mounts)
  - [Idle Management](#idle-management)
  - [Plugins](#plugins)
  - [Integrations](#integrations)
  - [T3 Code GUI](#t3-code-gui)
- [Commands](#commands)
- [Plugins](#plugins-1)
- [Agents](#agents)
- [Building from Source](#building-from-source)
- [Contributing](#contributing)
- [License](#license)

---

## Requirements

- **macOS** (Intel or Apple Silicon) — Linux/Windows is not supported
- [Colima](https://github.com/abiosoft/colima) + [Docker](https://docs.docker.com/desktop/install/mac-install/) (or Docker Desktop)
- Authentication for your chosen agent — run `claude auth` or `gh auth login` **inside** the VM after first launch

## Installation

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/sisimomo/aivm/main/install.sh | sh
```

This downloads the latest release binary for your architecture (`darwin/amd64` or `darwin/arm64`), installs it to `/usr/local/bin/aivm`, and creates a default config at `~/.aivm/aivm.yaml`.

### From source

```bash
git clone https://github.com/sisimomo/aivm.git
cd aivm
make install
```

Verify:

```bash
aivm version
```

---

## Quick Start

1. **Edit your config:**

   ```bash
   nano ~/.aivm/aivm.yaml
   ```

   Set `agents.enabled` to `claude` or `copilot`.

2. **Launch an agent from any directory under your dev root:**

   ```bash
   cd ~/dev/my-project
   aivm
   ```

   On the first run, aivm starts the VM, bootstraps all plugins, and drops you into an interactive agent session. Subsequent runs skip the bootstrap (idempotent).

3. **Authenticate inside the VM** (first time only):

   ```bash
   aivm ssh
   # then: claude auth   OR   gh auth login
   ```

---

## Configuration

Config file: `~/.aivm/aivm.yaml`  
Use [`aivm.example.yaml`](aivm.example.yaml) as a reference.

```yaml
# Which agent to launch (claude | copilot)
agents:
  enabled: claude

vm:
  cpus: 4
  memory: "8GB"
  disk: "60GB"
  mounts:
    - "~/dev:rw"
    - "~/.ssh:ro"

idle:
  stop_timeout: 5m
  delete_timeout: 5m
```

### VM Resources

| Key | Default | Description |
|-----|---------|-------------|
| `vm.cpus` | `4` | Number of vCPUs |
| `vm.memory` | `"8GB"` | RAM (supports `MB`, `GB`) |
| `vm.disk` | `"60GB"` | Disk size (supports `MB`, `GB`, `TB`) |

### Mounts

Directories listed under `vm.mounts` are bind-mounted into the VM. Format: `<host_path>:<mode>` where mode is `rw` (read-write) or `ro` (read-only). `~` expands to your home directory.

```yaml
vm:
  mounts:
    - "~/dev:rw"
    - "~/.ssh:ro"
    - "~/work:rw"
```

### Idle Management

aivm runs an idle monitor daemon that watches session activity and automatically tears down the VM when unused:

```yaml
idle:
  stop_timeout: 5m    # stop VM this long after last active session ends
  delete_timeout: 5m  # delete the stopped VM after this additional wait
```

Set either value to `0` to disable that stage. Idle monitoring is automatically disabled when [T3 Code](#t3-code-gui) is enabled.

### Plugins

Plugins install toolchains inside the VM during bootstrap. They are resolved in dependency order and are idempotent (skipped if already installed).

**Override built-in plugin defaults:**

```yaml
plugins:
  config:
    java:
      version: "21"
      distribution: temurin
    nodejs:
      version: "20"
    python:
      version: "3.11.9"
    golang:
      version: "go1.22.4"
    maven:
      version: "3.9.6"
```

**Add custom plugins or restrict the enabled set:**

```yaml
plugins:
  enabled:
    - system
    - nodejs
    - rust       # your custom plugin

  define:
    rust:
      description: "Rust toolchain via rustup"
      dependencies: [system]
      skip_if: |
        rustc --version >/dev/null 2>&1
      setup: |
        curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
```

### Integrations

Integrations run a configure script when a specific plugin is installed **and** a specific agent is active. Use them to wire tools into an agent's context (e.g. register an MCP server).

```yaml
integrations:
  - from: my-tool       # plugin that must be installed
    to: claude          # agent that must be active
    skip_if: |
      my-tool status --agent claude >/dev/null 2>&1
    configure: |
      my-tool configure --agent claude
```

Omit `from` to run the integration whenever a given agent is active, regardless of plugins.

### T3 Code GUI

When enabled, `aivm launch` starts the [T3 Code](https://t3.tools/) web UI inside the VM and port-forwards it to your host:

```yaml
t3code:
  enable: true
  port: 3773   # http://localhost:3773
```

> **Note:** `agents.enabled` is still required — T3 Code is a frontend, not an agent. Idle monitoring is disabled in this mode; use `aivm stop` to shut down explicitly.

---

## Commands

```
aivm [directory]       Launch the configured AI agent (default command)
```

| Command | Description |
|---------|-------------|
| `aivm` | Start VM + services, then launch agent in current directory |
| `aivm start` | Start VM and services only |
| `aivm stop` | Stop VM and services (disk preserved) |
| `aivm restart` | Stop then start VM and services |
| `aivm destroy` | Delete the VM entirely (host state in `~/.aivm` is preserved) |
| `aivm status` | Show VM and service status |
| `aivm ssh` | Open an interactive shell in the VM |
| `aivm logs [service]` | Show logs for a service (`mcpjungle` · `monitor` · `bootstrap` · `colima`) |
| `aivm rebuild-image` | Rebuild the base VM image by re-running full bootstrap from scratch |
| `aivm version` | Print version |

**Global flags:**

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to `aivm.yaml` (default: `~/.aivm/aivm.yaml`) |
| `--debug` | Enable verbose debug logging |

### `aivm rebuild-image`

Re-runs the full bootstrap process on a clean blank VM, unconditionally installing all plugins. Useful after adding new plugins or when the image has drifted.

```bash
aivm rebuild-image          # prompts if active sessions exist
aivm rebuild-image --force  # stop active sessions without prompting
```

---

## Plugins

Built-in plugins (all idempotent, resolved in dependency order):

| Plugin | Description | Default Version |
|--------|-------------|-----------------|
| `system` | Base packages via apt (`git`, `curl`, `jq`, etc.) | — |
| `nodejs` | Node.js via nvm | `22` |
| `java` | Temurin JDK via Adoptium | `25` |
| `maven` | Apache Maven | `3.9.9` |
| `python` | Python via pyenv | `3.12.7` |
| `uv` | uv — fast Python package manager | latest |
| `golang` | Go via gvm | `go1.24.0` |
| `gh` | GitHub CLI | latest |
| `t3code` | T3 Code web GUI (installed when `t3code.enable: true`) | latest |

Agent plugins are registered automatically based on `agents.enabled`:

| Agent plugin | Description |
|--------------|-------------|
| `claude` | Claude Code CLI (depends on `nodejs`) |
| `copilot` | GitHub Copilot CLI (depends on `system`, `gh`) |

---

## Agents

### Claude Code

```yaml
agents:
  enabled: claude
```

Runs `claude --dangerously-skip-permissions --mcp-config "$HOME/.claude/mcp-config.json"`. Claude's project history is persisted to `~/.aivm/.claude/projects/` on the host.

Authenticate inside the VM once:

```bash
aivm ssh
claude auth
```

### GitHub Copilot

```yaml
agents:
  enabled: copilot
```

Runs `gh copilot --yolo`. Session state is persisted to `~/.aivm/.copilot/session-state/`.

Authenticate inside the VM once:

```bash
aivm ssh
gh auth login
```

### Custom Agents

Override a built-in agent's launch command or install steps:

```yaml
agents:
  define:
    copilot:
      launch_command: "gh copilot suggest"
```

---

## Building from Source

```bash
git clone https://github.com/sisimomo/aivm.git
cd aivm

# Build binary to bin/aivm
make build

# Install to /usr/local/bin/aivm
make install

# Run unit tests
make test

# Lint
make vet

# Format
make fmt
```

**Release snapshot (requires [goreleaser](https://goreleaser.com/)):**

```bash
make release-snapshot
```

---

## Contributing

Pull requests are welcome. For major changes, please open an issue first.

1. Fork the repo and create a feature branch
2. Make your changes with tests where applicable
3. Run `make fmt vet test` to verify
4. Open a pull request against `main`

---

## License

[MIT](LICENSE)
