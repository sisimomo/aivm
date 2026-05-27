# aivm

> Launch AI agents in a secure, disposable Linux runtime (Colima or Docker) — with one command.

[![CI](https://github.com/sisimomo/aivm/actions/workflows/ci.yml/badge.svg)](https://github.com/sisimomo/aivm/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](go.mod)
[![macOS only](https://img.shields.io/badge/platform-macOS-lightgrey.svg)](#requirements)

## Demo

https://github.com/user-attachments/assets/fd87f402-459d-4957-bffa-f786d0eb357b

---

**aivm** manages the full lifecycle of a disposable Linux runtime dedicated to running AI coding agents. It runs on Colima by default or Docker via the Docker backend, bootstraps the runtime with your choice of toolchain plugins, optionally starts Docker Compose services (e.g. MCP servers) tied to the VM lifecycle, keeps sessions alive while you work, and auto-cleans up when you're idle.

Supported agents: **Claude Code** (`claude`) · **GitHub Copilot** (`copilot`) · **OpenCode** (`opencode`)

---

## Table of Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
  - [VM Resources](#vm-resources)
  - [VM Backend](#vm-backend)
  - [Mounts](#mounts)
  - [Idle Management](#idle-management)
  - [Plugins](#plugins)
  - [Cocoindex Code](#cocoindex-code)
  - [Skills](#skills)
  - [Integrations](#integrations)
  - [Compose File](#compose-file)
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
- [Colima](https://github.com/abiosoft/colima) + [Docker](https://docs.docker.com/desktop/install/mac-install/) for the default backend, or Docker Engine/Desktop for the Docker backend
- Authentication for your chosen agent is handled **inside** the VM after first launch

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

   Set `agents.enabled` to `claude`, `copilot`, or `opencode`.
   Keep `vm.backend: colima` for the default setup, or set `vm.backend: docker` and `vm.docker_image` to run on Docker directly.

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
# Which agent to launch (claude | copilot | opencode)
agents:
  enabled: claude

vm:
  cpus: 4
  memory: "8GB"
  disk: "60GB"
  backend: colima
  name: aivm
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

### VM Backend

| Key | Default | Description |
|-----|---------|-------------|
| `vm.backend` | `colima` | VM runtime: `colima` or `docker` |
| `vm.name` | `aivm` | VM identity (Colima profile name / Docker container name) |
| `vm.docker_image` | required for `docker` | Base image used when `vm.backend: docker` |

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

**mise-based tools (`mise-<tool>`):**

Any tool available in the [mise registry](https://mise-versions.jdx.dev/) can be installed by prefixing its name with `mise-`. For example:

```yaml
plugins:
  enabled:
    - mise-node
    - mise-go
    - mise-rust
    - mise-java
    - mise-python
```

The tool name after the `mise-` prefix is passed directly to `mise use --global <tool>@<version>`. See the [mise tool catalog](https://mise.jdx.dev/registry.html) for valid tool names.

**Override the version:**

```yaml
plugins:
  config:
    mise-node:
      version: "20"
    mise-java:
      version: "21"
    mise-go:
      version: "1.22.4"
```

The default version is `latest` unless overridden.

**Install multiple versions of the same tool:**

Use `extra_versions` to install additional versions alongside the global one. The `version` entry sets the global; extra versions are installed with `mise install` and are available for project-level `mise.toml` files to select.

```yaml
plugins:
  config:
    mise-node:
      version: "22"               # global — used when no mise.toml overrides
      extra_versions: ["20", "18"] # also installed; pick via mise.toml in projects
```

`SkipIf` is strict: setup is skipped only when **all** configured versions are already present. Adding a new entry to `extra_versions` will trigger re-installation on the next bootstrap.

**Add custom plugins or restrict the enabled set:**

```yaml
plugins:
  enabled:
    - system
    - mise-node
    - azure-cli       # your custom plugin

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

### Compose File

Point aivm at a standard `docker-compose.yml` to run services alongside the VM. All services are brought up with `docker compose up -d` when the VM starts, and torn down with `docker compose down` on stop. Named volumes are **never** deleted by aivm — they persist across VM recreations and `aivm destroy`.

```yaml
compose_file: ./docker-compose.yml
```

The path is resolved relative to `aivm.yaml`. The compose project name is set to the VM profile name (`vm.name`).

**Example `docker-compose.yml`:**

```yaml
services:
  metamcp:
    image: ghcr.io/metatool-ai/metamcp:latest
    ports:
      - "127.0.0.1:3000:3000"
    environment:
      AUTH_TOKEN: ${AUTH_TOKEN}  # source from .env or export before launching aivm
    restart: unless-stopped
```

**Security note:** Always use a strong, randomly generated secret for `AUTH_TOKEN`. Set it in a `.env` file (add `.env` to `.gitignore`) or export it before launching aivm. Never commit weak or placeholder credentials like "changeme".

Use your compose file's native `.env` file or `environment:` keys for variable substitution — no aivm-specific template variables are needed.

**Status display:** `aivm status` reports each service's running state.

**Log access:** `aivm logs` (no arguments) streams logs for all compose services via `docker compose logs -f`.

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
| `aivm destroy` | Delete the VM entirely (volumes and host state in `~/.aivm` are preserved) |
| `aivm status` | Show VM and service status |
| `aivm ssh` | Open an interactive shell in the VM |
| `aivm cp <src> <dst>` | Copy files or directories between host and VM (use `vm:` prefix for VM paths) |
| `aivm logs [service]` | Show logs for a service (`monitor` · `bootstrap` · `vm`) or all compose services (no arg) |
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

### `aivm cp`

Copy files or directories between the host and the VM. Prefix VM paths with `vm:`.

```bash
aivm cp vm:/home/user/file.txt ./local/          # copy file from VM to host
aivm cp ./local/file.txt vm:/home/user/          # copy file from host to VM
aivm cp -r vm:/home/user/dir/ ./local/dir/       # copy directory from VM to host
aivm cp -rf ./local/dir/ vm:/home/user/dir/      # copy directory to VM, overwrite
```

**Flags:**

| Flag                 | Description                                |
|----------------------|--------------------------------------------|
| `-r` / `--recursive` | Copy directories recursively               |
| `-f` / `--force`     | Overwrite destination if it already exists |

---

## Plugins

Built-in plugins (all idempotent, resolved in dependency order):

| Plugin | Description |
|--------|-------------|
| `system` | Base packages via apt (`git`, `curl`, `jq`, etc.) |
| `mise` | [mise-en-place](https://mise.jdx.dev/) — universal runtime version manager |
| `awscli` | AWS CLI v2 |
| `cocoindex-code` | [Cocoindex Code](https://github.com/cocoindex-io/cocoindex-code) — AST-based semantic code search MCP server |
| `skills` | Install agent skills via [`npx skills@latest`](https://github.com/vercel-labs/skills) (opt-in, disabled by default) |
| `t3code` | T3 Code web GUI (installed when `t3code.enable: true`) |

**`mise-<tool>` — dynamic tool plugins:**

Any tool in the [mise registry](https://mise-versions.jdx.dev/) is available as `mise-<tool>`:

| Example | Installs |
|---------|----------|
| `mise-node` | Node.js |
| `mise-go` | Go |
| `mise-java` | Java (Temurin by default) |
| `mise-rust` | Rust |
| `mise-python` | Python |
| `mise-maven` | Apache Maven |
| `mise-gradle` | Gradle |
| `mise-terraform` | HashiCorp Terraform |
| `mise-helm` | Helm |

See the full [mise tool catalog](https://mise.jdx.dev/registry.html#tools) for all available tool names.

**Config keys for `mise-<tool>` plugins:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `version` | string | `"latest"` | Version set as global via `mise use --global` |
| `extra_versions` | list | `[]` | Additional versions installed via `mise install` (not set as global) |

Agent are registered automatically based on `agents.enabled`:

| Agent plugin | Description                                  |
|--------------|----------------------------------------------|
| `claude` | Claude Code CLI (depends on `mise-node`)     |
| `copilot` | GitHub Copilot CLI (depends on `system`, `gh`) |
| `opencode` | OpenCode CLI (depends on `system`)           |

---

## Cocoindex Code

[Cocoindex Code](https://github.com/cocoindex-io/cocoindex-code) is an AST-based semantic code search tool that exposes a `search` MCP tool to your AI agent. When enabled, aivm installs `ccc` in the VM and automatically wires it as an MCP server for every active agent.

Add `cocoindex-code` to `plugins.enabled`:

```yaml
plugins:
  enabled:
    - cocoindex-code
```

That's it. aivm installs the `full` variant by default — local [Snowflake/snowflake-arctic-embed-xs](https://huggingface.co/Snowflake/snowflake-arctic-embed-xs) embeddings, no API key required.

### Cloud embedding provider

To use a cloud provider instead, set `variant: slim` and supply a `config:` block. Its contents are written verbatim as `~/.cocoindex_code/global_settings.yml` inside the VM — see [cocoindex-code's global settings docs](https://github.com/cocoindex-io/cocoindex-code#user-settings-cocoindex_codeglobal_settingsyml) and [embedding model reference](https://github.com/cocoindex-io/cocoindex-code#embedding-models) for all supported keys and providers:

```yaml
plugins:
  enabled:
    - cocoindex-code
  config:
    cocoindex-code:
      variant: slim
      config:
        embedding:
          model: voyage/voyage-code-3
        envs:
          VOYAGE_API_KEY: your-api-key
```

### MCP auto-wiring

When `cocoindex-code` is installed and an agent is active, aivm runs the appropriate integration at bootstrap time:

| Agent | Integration |
|-------|-------------|
| `claude` | `claude mcp add cocoindex-code -- ccc mcp` |
| `copilot` | Patches `~/.copilot/mcp-config.json` |
| `opencode` | Patches `~/.config/opencode/opencode.json` |

Each integration is idempotent — it is skipped if the MCP server is already registered.

---

## Skills

The `skills` plugin integrates with the [skills CLI](https://github.com/vercel-labs/skills) — an open ecosystem tool that installs community-published agent skills (SKILL.md files) from any public GitHub repository into your AI agent's skills directory.

The plugin is **disabled by default**. Enable it by adding `skills` to `plugins.enabled` and listing the open-source skill repositories you want:

```yaml
plugins:
  enabled:
    - skills
  config:
    skills:
      sources:
        - repo: mattpocock/skills
```

This runs `npx skills@latest add mattpocock/skills --global --all` inside the VM, installing every skill from the repository for all configured agents into `~/<agent>/skills/`.

To install only specific skills from a repository, provide an explicit `skills` list:

```yaml
plugins:
  config:
    skills:
      sources:
        - repo: mattpocock/skills
          skills: [tdd, grill-me]
```

Multiple sources are supported — each entry is resolved independently:

```yaml
plugins:
  config:
    skills:
      sources:
        - repo: mattpocock/skills          # all skills from this repo
        - repo: another-author/skills
          skills: [tdd]                    # only tdd from this repo
```

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

### OpenCode

```yaml
agents:
  enabled: opencode
```

Runs `"$HOME/.opencode/bin/opencode"`. The binary is installed with the upstream `curl -fsSL https://opencode.ai/install | bash` flow, with PATH modification disabled so aivm controls the runtime environment.

Authenticate inside the VM on first launch if prompted.

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
