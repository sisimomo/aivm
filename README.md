# aivm

> Launch AI agents in a secure, disposable Linux runtime (Colima or Docker) —
> with one command.

[![CI](https://github.com/sisimomo/aivm/actions/workflows/ci.yml/badge.svg)](https://github.com/sisimomo/aivm/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](go.mod)
[![macOS only](https://img.shields.io/badge/platform-macOS-lightgrey.svg)](#requirements)

## Demo

<https://github.com/user-attachments/assets/fd87f402-459d-4957-bffa-f786d0eb357b>

---

**aivm** manages the full lifecycle of a disposable Linux runtime dedicated to
running AI coding agents. It runs on Colima by default or Docker via the Docker
backend, bootstraps the runtime with your choice of toolchain plugins,
optionally starts Docker Compose services (e.g. MCP servers) tied to the VM
lifecycle, keeps sessions alive while you work, and auto-cleans up when you're
idle.

Supported agents: **Claude Code** (`claude`) · **GitHub Copilot** (`copilot`) ·
**Cursor Agent** (`cursor`) · **OpenCode** (`opencode`)

---

## Table of Contents

- [Requirements](#requirements)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
  - [VM Resources](#vm-resources)
  - [VM Backend](#vm-backend)
  - [Mounts](#mounts)
  - [Session host environment](#session-host-environment)
  - [Idle Management](#idle-management)
  - [Plugins](#plugins)
  - [Cocoindex Code](#cocoindex-code)
  - [Context7](#context7)
  - [Skills](#skills)
  - [Integrations](#integrations)
  - [Compose File](#compose-file)
  - [T3 Code GUI](#t3-code-gui)
- [Commands](#commands)
- [Tool integration](#tool-integration)
- [Built-in Plugins](#built-in-plugins)
- [Agents](#agents)
- [Building from Source](#building-from-source)
- [Contributing](#contributing)
- [License](#license)

---

## Requirements

- **macOS** (Intel or Apple Silicon) — Linux/Windows is not supported
- [Colima](https://github.com/abiosoft/colima) +
- [Docker](https://docs.docker.com/desktop/install/mac-install/) for the default
- backend, or Docker Engine/Desktop for the Docker backend
- Authentication for your chosen agent is handled **inside** the VM after first
- launch

## Installation

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/sisimomo/aivm/main/install.sh | sh
```

This downloads the latest release binary for your architecture (`darwin/amd64`
or `darwin/arm64`), installs it to `/usr/local/bin/aivm`, and creates a default
config at `~/.aivm/aivm.yaml`.

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

   Set `agents.default` and enable one or more entries under `agents.define`.
   Keep `vm.backend: colima` for the default setup, or set `vm.backend: docker`
   and `vm.docker_image` to run on Docker directly.

2. **Launch an agent from any directory under your dev root:**

   ```bash
   cd ~/dev/my-project
   aivm
   ```

   On the first run, aivm starts the VM, bootstraps all plugins, and drops you
   into an interactive agent session. Subsequent runs skip the bootstrap
   (idempotent).

---

## Configuration

Config file: `~/.aivm/aivm.yaml`  
Use [`aivm.example.yaml`](aivm.example.yaml) as a reference.

```yaml
# Which built-in agent to launch (claude | copilot | cursor | opencode)
agents:
  default: claude
  define:
    claude:
      enable: true

vm:
  cpus: 4
  memory: "8GB"
  disk: "60GB"
  backend: colima
  name: aivm
  mounts:
    - "~/dev:rw"
    - "~/.ssh:ro"
  # session_env:
  #   MY_TOOL_SESSION_ID: "${MY_TOOL_SESSION_ID}"
  #   CI_JOB_ID: "${CI_JOB_ID}"

idle:
  stop_timeout: 5m
  delete_timeout: 5m

# aivm CLI terminal verbosity (trace | debug | info | warn | error)
log_level: info   # all levels are always written to logs/aivm.log
```

Override at runtime with `--log-level` or `AIVM_LOG_LEVEL` (see
[Commands](#commands)).

At the default `info` level the terminal shows milestones and warnings; use
`debug` for operational detail or `trace` for subprocess output. The log file at
`~/.aivm/logs/aivm.log` always records `trace` and above.

### VM Resources

| Key | Default | Description |
| --- | --- | --- |
| `vm.cpus` | `4` | Number of vCPUs |
| `vm.memory` | `"8GB"` | RAM (supports `MB`, `GB`) |
| `vm.disk` | `"60GB"` | Disk size (supports `MB`, `GB`, `TB`) |

### VM Backend

| Key | Default | Description |
| --- | --- | --- |
| `vm.backend` | `colima` | VM runtime: `colima` or `docker` |
| `vm.name` | `aivm` | VM identity (Colima profile / Docker container name) |
| `vm.docker_image` | for `docker` | Base image for docker backend |

### Mounts

Directories listed under `vm.mounts` are bind-mounted into the VM. Format:
`<host_path>:<mode>` where mode is `rw` (read-write) or `ro` (read-only). `~`
expands to your home directory.

```yaml
vm:
  mounts:
    - "~/dev:rw"
    - "~/.ssh:ro"
    - "~/work:rw"
```

### Session host environment

`vm.session_env` maps environment variable names to values, using the same
`${HOST_VAR}` expansion as `vm.env`. On each agent or shell session (`aivm`,
`aivm agent -- …`, or `aivm ssh`), values are resolved from the invoking host
process and exported inside the VM for that session only.

Use this for context that exists in the terminal where you run aivm — session
IDs, job metadata, tool-specific identifiers — without hardcoding values in
config or the VM image.

Unlike persistent `vm.env` (applied at bootstrap/sync and shared by every login
shell), `vm.session_env` is resolved fresh per session and is not written into
the VM.

```yaml
vm:
  session_env:
    MY_TOOL_SESSION_ID: "${MY_TOOL_SESSION_ID}"
    CI_JOB_ID: "${CI_JOB_ID}"
```

Every configured key is exported each session. Missing or empty host references
expand to empty strings.

### Idle Management

aivm runs an idle monitor daemon that watches session activity and automatically
tears down the VM when unused:

```yaml
idle:
  stop_timeout: 5m    # stop VM this long after last active session ends
  delete_timeout: 5m  # delete the stopped VM after this additional wait
```

Set either value to `0` to disable that stage. Idle monitoring is automatically
disabled when [T3 Code](#t3-code-gui) is enabled.

### Plugins

Plugins install toolchains inside the VM during bootstrap. They are resolved in
dependency order and are idempotent (skipped if already installed).

**mise-based tools (`mise-<tool>`):**

Any tool available in the [mise registry](https://mise-versions.jdx.dev/) can be
installed by prefixing its name with `mise-`. For example:

```yaml
plugins:
  enabled:
    - mise-node
    - mise-go
    - mise-rust
    - mise-java
    - mise-python
```

The tool name after the `mise-` prefix is passed directly to `mise use --global
<tool>@<version>`. See the [mise tool
catalog](https://mise.jdx.dev/registry.html) for valid tool names.

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

Use `extra_versions` to install additional versions alongside the global one.
The `version` entry sets the global; extra versions are installed with `mise
install` and are available for project-level `mise.toml` files to select.

```yaml
plugins:
  config:
    mise-node:
      version: "22"               # global — used when no mise.toml overrides
      extra_versions: ["20", "18"] # also installed; pick via mise.toml in projects
```

`SkipIf` is strict: setup is skipped only when **all** configured versions are
already present. Adding a new entry to `extra_versions` will trigger
re-installation on the next bootstrap.

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

Integrations run a configure script when a specific plugin is installed **and**
a specific agent is active. Use them to wire tools into an agent's context (e.g.
register an MCP server).

```yaml
integrations:
  - from: my-tool       # plugin that must be installed
    to: claude          # agent that must be active
    skip_if: |
      my-tool status --agent claude >/dev/null 2>&1
    configure: |
      my-tool configure --agent claude
```

Omit `from` to run the integration whenever a given agent is active, regardless
of plugins.

### Compose File

Point aivm at a standard `docker-compose.yml` to run services alongside the VM.
All services are brought up with `docker compose up -d` when the VM starts, and
torn down with `docker compose down` on stop. Named volumes are **never**
deleted by aivm — they persist across VM recreations and `aivm destroy`.

```yaml
compose_file: ./docker-compose.yml
```

The path is resolved relative to `aivm.yaml`. The compose project name is set to
the VM profile name (`vm.name`).

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

**Security note:** Always use a strong, randomly generated secret for
`AUTH_TOKEN`. Set it in a `.env` file (add `.env` to `.gitignore`) or export it
before launching aivm. Never commit weak or placeholder credentials like
"changeme".

Use your compose file's native `.env` file or `environment:` keys for variable
substitution — no aivm-specific template variables are needed.

**Status display:** `aivm status` reports each service's running state.

**Log access:** `aivm logs` tails `logs/aivm.log` (orchestration and subprocess
output at trace). `aivm logs monitor` tails the idle monitor daemon.

### T3 Code GUI

When enabled, `aivm launch` starts the [T3 Code](https://t3.tools/) web UI
inside the VM and port-forwards it to your host:

```yaml
t3code:
  enable: true
  port: 3773   # http://localhost:3773
```

> **Note:** `agents.default` plus at least one `agents.define.<name>.enable:
> true` entry is still required — T3 Code is a frontend, not an agent. Idle
> monitoring is disabled in this mode; use `aivm stop` to shut down explicitly.

---

## Commands

```text
aivm [directory]       Launch the configured AI agent (default command)
```

| Command | Description |
| --- | --- |
| `aivm` | Start VM + services, then launch agent in current directory |
| `aivm agent -- <args>` | Agent CLI with custom args (`launch_args` skipped) |
| `aivm start` | Start VM and services only |
| `aivm stop` | Stop VM and services (disk preserved) |
| `aivm restart` | Stop then start VM and services |
| `aivm destroy` | Delete VM (volumes and `~/.aivm` host state preserved) |
| `aivm status` | Show VM and service status |
| `aivm ssh` | Open an interactive shell in the VM |
| `aivm cp <src> <dst>` | Copy host ↔ VM files (`vm:` prefix for VM paths) |
| `aivm logs [component]` | Tail logs (`aivm` · `monitor`; default `aivm`) |
| `aivm rebuild-image` | Rebuild base VM image via full bootstrap |
| `aivm version` | Print version |

**Global flags:**

| Flag | Description |
| --- | --- |
| `--config <path>` | Path to `aivm.yaml` (default: `~/.aivm/aivm.yaml`) |
| `--agent <name>` | Agent to use (overrides `agents.default`) |
| `--log-level` | `trace`–`error` (default `info`; overrides env/config) |

Environment variable `AIVM_LOG_LEVEL` and config key `log_level:` use the same
values. Precedence: flag → env → config.

`--log-level` may appear before the subcommand (`aivm --log-level error agent --
…`) or after `agent` but before `--` (`aivm agent --log-level error -- …`).
Flags after the first `--` are passed to the agent CLI, not aivm.

**Interactive vs passthrough:**

| Invocation | Uses `launch_args` | Agent arguments |
| --- | --- | --- |
| `aivm` (default) | Yes | From config (e.g. `--dangerously-skip-permissions`) |
| `aivm agent -- <args>` | No | Your args after `--` |

Use `--agent <name>` with either path to override `agents.default` (agent must
be enabled in config).

### Tool integration

External tools (Cursor custom agent command, OpenCode, etc.) can run agents
inside aivm by setting a **command prefix**—no PATH shims. The tool appends its
own CLI flags after the trailing `--`; do not run the prefix alone (aivm
requires at least one argument after `--`).

```bash
# Cursor — custom agent command prefix (appends flags after --)
export AIVM_LOG_LEVEL=error   # optional: quiet bootstrap (failures still print)
aivm --agent cursor agent --
```

```bash
# OpenCode — same pattern (OpenCode appends its subcommand/flags after --)
export AIVM_LOG_LEVEL=error
aivm --agent opencode agent --
```

The `--` separator is **required** between aivm flags and agent arguments.
Example (you can also run this directly):

```bash
aivm --agent cursor agent -- -p "explain this repo"
```

With `AIVM_LOG_LEVEL=error`, progress steps are hidden so the tool sees mostly
agent output. Cold VM start can take one to two minutes with little output until
the agent runs or an error is reported.

### `aivm rebuild-image`

Re-runs the full bootstrap process on a clean blank VM, unconditionally
installing all plugins. Useful after adding new plugins or when the image has
drifted.

```bash
aivm rebuild-image          # prompts if active sessions exist
aivm rebuild-image --force  # stop active sessions without prompting
```

### `aivm cp`

Copy files or directories between the host and the VM. Prefix VM paths with
`vm:`.

```bash
aivm cp vm:/home/user/file.txt ./local/          # copy file from VM to host
aivm cp ./local/file.txt vm:/home/user/          # copy file from host to VM
aivm cp -r vm:/home/user/dir/ ./local/dir/       # copy directory from VM to host
aivm cp -rf ./local/dir/ vm:/home/user/dir/      # copy directory to VM, overwrite
```

**Flags:**

| Flag | Description |
| --- | --- |
| `-r` / `--recursive` | Copy directories recursively |
| `-f` / `--force` | Overwrite destination if it already exists |

---

## Built-in Plugins

Built-in plugins (all idempotent, resolved in dependency order):

| Plugin | Description |
| --- | --- |
| `system` | Base packages via apt (`git`, `curl`, `jq`, etc.) |
| `mise` | [mise-en-place](https://mise.jdx.dev/) — universal runtime version manager |
| `awscli` | AWS CLI v2 |
| `cocoindex-code` | [Cocoindex Code](https://github.com/cocoindex-io/cocoindex-code) — AST-based semantic code search MCP server |
| `context7` | [Context7](https://context7.com) — up-to-date library documentation via find-docs skill |
| `skills` | Install agent skills via [`npx skills@latest`](https://github.com/vercel-labs/skills) (opt-in, disabled by default) |
| `t3code` | T3 Code web GUI (installed when `t3code.enable: true`) |

**`mise-<tool>` — dynamic tool plugins:**

Any tool in the [mise registry](https://mise-versions.jdx.dev/) is available as
`mise-<tool>`:

| Example | Installs |
| --- | --- |
| `mise-node` | Node.js |
| `mise-go` | Go |
| `mise-java` | Java (Temurin by default) |
| `mise-rust` | Rust |
| `mise-python` | Python |
| `mise-maven` | Apache Maven |
| `mise-gradle` | Gradle |
| `mise-terraform` | HashiCorp Terraform |
| `mise-helm` | Helm |

See the full [mise tool catalog](https://mise.jdx.dev/registry.html#tools) for
all available tool names.

**Config keys for `mise-<tool>` plugins:**

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `version` | string | `"latest"` | Global version via `mise use --global` |
| `extra_versions` | list | `[]` | Extra versions via `mise install` |

Enabled agent plugins are registered automatically based on
`agents.define.<name>.enable: true`:

| Agent plugin | Description |
| --- | --- |
| `claude` | Claude Code CLI (depends on `mise-node`) |
| `copilot` | GitHub Copilot CLI |
| `cursor` | Cursor Agent CLI (depends on `system`) |
| `opencode` | OpenCode CLI (depends on `system`) |

---

## Cocoindex Code

[Cocoindex Code](https://github.com/cocoindex-io/cocoindex-code) is an AST-based
semantic code search tool that exposes a `search` MCP tool to your AI agent.
When enabled, aivm installs `ccc` in the VM and automatically wires it as an MCP
server for every active agent.

Add `cocoindex-code` to `plugins.enabled`:

```yaml
plugins:
  enabled:
    - cocoindex-code
```

That's it. aivm installs the `full` variant by default — local
[Snowflake/snowflake-arctic-embed-xs](https://huggingface.co/Snowflake/snowflake-arctic-embed-xs)
embeddings, no API key required.

### Cloud embedding provider

To use a cloud provider instead, set `variant: slim` and supply a `config:`
block. Its contents are written verbatim as
`~/.cocoindex_code/global_settings.yml` inside the VM — see [cocoindex-code's
global settings
docs](https://github.com/cocoindex-io/cocoindex-code#user-settings-cocoindex_codeglobal_settingsyml)
and [embedding model
reference](https://github.com/cocoindex-io/cocoindex-code#embedding-models) for
all supported keys and providers:

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

When `cocoindex-code` is installed and an agent is active, aivm runs the
appropriate integration at bootstrap time:

| Agent | Integration |
| --- | --- |
| `claude` | `claude mcp add cocoindex-code -- ccc mcp` |
| `copilot` | Patches `~/.copilot/mcp-config.json` |
| `opencode` | Patches `~/.config/opencode/opencode.json` |

Each integration is idempotent — it is skipped if the MCP server is already
registered.

---

## Context7

[Context7](https://context7.com) provides up-to-date library documentation for
AI coding agents. When enabled, aivm installs the `ctx7` CLI and the `find-docs`
skill for every supported agent via `npx skills@latest`.

Add `context7` to `plugins.enabled`:

```yaml
plugins:
  enabled:
    - context7
```

No API key required — `ctx7 library` and `ctx7 docs` work without
authentication. For higher rate limits, set `CONTEXT7_API_KEY` via `vm.env`
(supports `${HOST_VAR}` expansion from your host shell):

```yaml
plugins:
  enabled:
    - context7

vm:
  env:
    CONTEXT7_API_KEY: "${CONTEXT7_API_KEY}"
```

---

## Skills

The `skills` plugin integrates with the [skills
CLI](https://github.com/vercel-labs/skills) — an open ecosystem tool that
installs community-published agent skills (SKILL.md files) from any public
GitHub repository into your AI agent's skills directory.

The plugin is **disabled by default**. Enable it by adding `skills` to
`plugins.enabled` and listing the open-source skill repositories you want:

```yaml
plugins:
  enabled:
    - skills
  config:
    skills:
      sources:
        - repo: mattpocock/skills
```

This runs `npx skills@latest add mattpocock/skills --global --all` inside the
VM, installing every skill from the repository for all configured agents into
`~/<agent>/skills/`.

To install only specific skills from a repository, provide an explicit `skills`
list:

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
  default: claude
  define:
    claude:
      enable: true
```

SSH in the VM and runs `claude --dangerously-skip-permissions`. Claude's project
history is persisted to `~/.aivm/.claude/projects/` on the host.

Authenticate inside the VM once:

```bash
aivm ssh
claude auth
```

### GitHub Copilot

```yaml
agents:
  default: copilot
  define:
    copilot:
      enable: true
```

SSH in the VM and runs `copilot --yolo`. Session state is persisted to
`~/.aivm/.copilot/session-state/`.

Authenticate inside the VM once:

```bash
aivm ssh
gh auth login
```

### Cursor Agent

```yaml
agents:
  default: cursor
  define:
    cursor:
      enable: true
```

SSH in the VM and runs `agent`. aivm installs Cursor with the upstream `curl
https://cursor.com/install -fsS | bash` flow, keeps `~/.local/bin` on PATH for
login shells, and persists Cursor CLI state in `~/.aivm/.cursor/`.

Authenticate inside the VM once:

```bash
aivm ssh
agent login
```

### OpenCode

```yaml
agents:
  default: opencode
  define:
    opencode:
      enable: true
```

SSH in the VM and runs `opencode`. The binary is installed with the upstream
`curl -fsSL https://opencode.ai/install | bash` flow, with PATH modification
disabled so aivm controls the runtime environment.

Authenticate inside the VM on first launch if prompted.

### Custom Agents

Each agent has a `cli_command` (binary in the VM) and optional `launch_args`
used only by the interactive shortcut (`aivm`). Override install steps or launch
behavior:

```yaml
agents:
  define:
    copilot:
      cli_command: copilot
      launch_args: "suggest"
```

Run a one-off prompt without `launch_args` (forwards only the args after `--`):

```bash
aivm agent -- -p "Refactor utils.js to use arrow functions"
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
