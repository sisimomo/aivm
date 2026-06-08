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
  - [Integrations](#integrations)
  - [Compose File](#compose-file)
  - [T3 Code GUI](#t3-code-gui)
- [Plugins](#plugins)
  - [Available plugins](#available-plugins)
  - [Customize built-in plugins](#customize-built-in-plugins)
  - [Create a custom plugin](#create-a-custom-plugin)
- [Commands](#commands)
- [Tool integration](#tool-integration)
- [Agents](#agents)
- [Building from Source](#building-from-source)
- [Contributing](#contributing)
- [License](#license)

---

## Requirements

- **macOS** (Intel or Apple Silicon) — Linux/Windows is not supported
- [Colima](https://github.com/abiosoft/colima) with
  [Docker](https://docs.docker.com/desktop/install/mac-install/) for the default
  `colima` backend, or Docker Engine/Desktop for the `docker` backend
- Authentication for your chosen agent is handled **inside** the VM after first
  launch

## Installation

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/sisimomo/aivm/main/install.sh | sh
```

This downloads the latest release binary for your architecture (`darwin/amd64`
or `darwin/arm64`) and installs it to `~/.local/bin/aivm` (override with
`AIVM_INSTALL_DIR`). Copy [`aivm.example.yaml`](aivm.example.yaml) to
`~/.aivm/aivm.yaml` on first use, or run `make install` from source to create
it automatically.

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

   Set `agents.enabled` and optionally `agents.default`.
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
  enabled:
    - claude

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

### Persistent VM environment

`vm.env` injects environment variables into the VM on bootstrap and on config
sync. Values support `${HOST_VAR}` expansion from the host shell. Unlike
`vm.session_env`, these are persisted in the VM and shared by every shell
session.

```yaml
vm:
  env:
    CONTEXT7_API_KEY: "${CONTEXT7_API_KEY}"
    MY_API_TOKEN: "${MY_API_TOKEN}"
```

Changes to `vm.env` are applied in place on the next `aivm` run — no VM
recreation required.

### VM type and recreation prompt

| Key | Default | Description |
| --- | --- | --- |
| `vm.type` | _(auto)_ | Colima hypervisor: `vz`, `qemu`, or omit for auto |
| `vm.recreate_prompt_after` | `"7d"` | Prompt to recreate VM after this age |

### Idle Management

aivm runs an idle monitor daemon that watches session activity and automatically
tears down the VM when unused:

```yaml
idle:
  stop_timeout: 5m      # stop VM this long after last active session ends
  delete_timeout: 5m    # delete the stopped VM after this additional wait
  poll_interval: 30s    # how often the idle monitor checks session activity
```

Set `stop_timeout` or `delete_timeout` to `0` to disable that stage. Idle
monitoring is automatically disabled when [T3 Code](#t3-code-gui) is enabled.

Plugin installation, customization, and authoring are documented in
[Plugins](#plugins).

### Integrations

Integrations run a `configure` shell script after bootstrap when their
conditions are met: a specific plugin is installed (`from`) **and** a specific
agent is active (`to`). Use them to wire tools into an agent's context (e.g.
register an MCP server).

```yaml
integrations:
  - name: my-tool-claude   # required when from is empty; optional otherwise
    from: my-tool          # plugin that must be installed (omit for agent-only)
    to: claude             # agent that must be active
    configure: |
      my-tool configure --agent claude
```

Omit `from` to run whenever the `to` agent is active, regardless of plugins
(`name` is required in that case).

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

When enabled, `aivm start` (or the default `aivm` command, which starts the VM
first) launches the [T3 Code](https://t3.tools/) web UI inside the VM and
port-forwards it to your host. The normal agent terminal still opens when you run
`aivm` — T3 Code runs alongside it as a browser-accessible frontend.

```yaml
t3code:
  enable: true
  port: 3773   # http://localhost:3773
```

`aivm status` shows the access URL and pairing token. The `t3code` plugin is
auto-installed when this mode is enabled.

> **Note:** At least one entry in `agents.enabled` is required; `agents.default`
> is inferred automatically when only one agent is enabled. Idle monitoring is
> disabled in this mode; use `aivm stop` to shut down explicitly.

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
| `aivm recreate` | Destroy VM and re-run full bootstrap (see below) |
| `aivm status` | Show VM, compose service, and T3 Code status |
| `aivm ssh` | Open an interactive shell in the VM |
| `aivm cp <src> <dst>` | Copy host ↔ VM files (`vm:` prefix for VM paths) |
| `aivm logs [component]` | Tail logs (`aivm` · `monitor`; default `aivm`) |
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

### `aivm recreate`

Destroys the current VM and re-runs full bootstrap on a fresh one, unconditionally
installing all plugins. Useful after adding new plugins or when the image has
drifted.

```bash
aivm recreate          # prompts if active sessions exist
aivm recreate --force  # stop active sessions without prompting
```

**Environment overrides:**

| Variable | Description |
| --- | --- |
| `AIVM_LOG_LEVEL` | Same values as `--log-level` |
| `AIVM_STATE_DIR` | Override `~/.aivm` state directory |
| `AIVM_INSTALL_DIR` | Install destination for `install.sh` / `make install` |

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

## Plugins

Plugins install software in the VM during bootstrap. Enable them in `aivm.yaml`:

```yaml
plugins:
  enabled:
    - system        # on by default
    - mise-node
    - awscli
```

Dependencies install automatically — if you enable `cocoindex-code`, aivm also
installs `mise-uv`, `mise`, and `system` without you listing them.

### Available plugins

Only `system` is enabled by default. Everything else is opt-in under
`plugins.enabled`, except `t3code` which is added when `t3code.enable: true`.

| Plugin | Installs | How to enable |
| --- | --- | --- |
| `system` | apt: `git`, `curl`, `jq`, compilers, … | Default |
| `mise` | [mise](https://mise.jdx.dev/) version manager | Auto (via `mise-*`) |
| `mise-<tool>` | Any [mise registry](https://mise.jdx.dev/registry.html) tool | `plugins.enabled` |
| `awscli` | AWS CLI v2 | `plugins.enabled` |
| `cocoindex-code` | `ccc` CLI + semantic search skill | `plugins.enabled` |
| `context7` | `ctx7` CLI + `find-docs` skill | `plugins.enabled` |
| `skills` | Community skills from GitHub repos | `plugins.enabled` |
| `t3code` | [T3 Code](https://t3.tools/) web GUI | `t3code.enable: true` |

#### `system`

Base VM packages via `apt-get`. Required by nearly every other plugin.

#### `mise` and `mise-<tool>`

`mise` installs the mise runtime. `mise-<tool>` is a family of plugins — one per
tool in the mise registry. aivm runs `mise use --global <tool>@<version>`.

| Plugin | Tool |
| --- | --- |
| `mise-node` | Node.js |
| `mise-python` | Python |
| `mise-uv` | uv |
| `mise-go` | Go |
| `mise-java` | Java (Temurin) |
| `mise-rust` | Rust |
| `mise-maven` | Maven |
| `mise-gradle` | Gradle |
| `mise-terraform` | Terraform |
| `mise-helm` | Helm |

```yaml
plugins:
  enabled:
    - mise-node
    - mise-go
```

Pin versions under `plugins.config` — see
[Customize built-in plugins](#customize-built-in-plugins).

#### `awscli`

Installs AWS CLI v2 with the official architecture-aware installer.

```yaml
plugins:
  enabled:
    - awscli
```

#### `cocoindex-code`

[Cocoindex Code](https://github.com/cocoindex-io/cocoindex-code) — AST-based
semantic code search (`ccc` CLI). Installs local embeddings by default (no API
key).

```yaml
plugins:
  enabled:
    - cocoindex-code
```

#### `context7`

[Context7](https://context7.com) — up-to-date library docs (`ctx7` CLI +
`find-docs` skill). No API key required; set `CONTEXT7_API_KEY` in `vm.env` for
higher rate limits.

```yaml
plugins:
  enabled:
    - context7
```

#### `skills`

Installs agent skills from public GitHub repos via the
[skills CLI](https://github.com/vercel-labs/skills). Requires a `sources` list
under `plugins.config` — see
[Customize built-in plugins](#customize-built-in-plugins).

```yaml
plugins:
  enabled:
    - skills
```

#### `t3code`

Installs the T3 Code npm package. Auto-enabled when `t3code.enable: true`.
Started by `aivm start` — see [T3 Code GUI](#t3-code-gui).

### Customize built-in plugins

Tune shipped plugins via `plugins.config` (settings) or `plugins.define`
(override install scripts). After changing `setup`, run
`aivm recreate`.

#### Settings with `plugins.config`

Values are passed to the plugin's setup script.

**`mise-<tool>` — pin versions**

| Key | Default | Description |
| --- | --- | --- |
| `version` | `"latest"` | Global version (`mise use --global`) |
| `extra_versions` | `[]` | Extra versions via `mise install` |

```yaml
plugins:
  config:
    mise-node:
      version: "22"
      extra_versions: ["20", "18"]
```

**`cocoindex-code` — install variant**

```yaml
plugins:
  enabled:
    - cocoindex-code
  config:
    cocoindex-code:
      variant: slim          # default: full (local embeddings)
      config:                # ~/.cocoindex_code/global_settings.yml
        embedding:
          model: voyage/voyage-code-3
        envs:
          VOYAGE_API_KEY: your-api-key
```

**`skills` — choose repositories**

```yaml
plugins:
  enabled:
    - skills
  config:
    skills:
      sources:
        - repo: mattpocock/skills          # all skills (--all)
        - repo: another-author/skills
          skills: [tdd, grill-me]          # named skills only
```

**`context7`**

No plugin config keys — use `vm.env` for `CONTEXT7_API_KEY`.

#### Override install scripts with `plugins.define`

Merge into a built-in plugin field-by-field. Non-empty fields replace the
built-in value.

```yaml
plugins:
  define:
    awscli:
      setup: |
        ARCH=$(uname -m)
        curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${ARCH}.zip" \
          -o /tmp/awscliv2.zip
        unzip -q /tmp/awscliv2.zip -d /tmp/awscliv2
        sudo /tmp/awscliv2/aws/install --update
```

| Field | Purpose |
| --- | --- |
| `description` | Label shown in bootstrap logs |
| `dependencies` | Plugins installed first |
| `path_entries` | Directories added to VM `PATH` |
| `defaults` | Default values merged with `plugins.config` |
| `agents` | Limit plugin to specific agents |
| `setup` | Install script |

### Create a custom plugin

Define a new plugin in `plugins.define` and list it in `plugins.enabled`.

```yaml
plugins:
  enabled:
    - my-tool

  define:
    my-tool:
      description: "My custom tool"
      dependencies: [system]
      path_entries:
        - "$HOME/.local/bin"
      setup: |
        curl -fsSL https://example.com/install.sh | bash
```

`setup` scripts must be safe to run on a fresh VM (use idempotent installers).

#### Schema

| Field | Required | Purpose |
| --- | --- | --- |
| `description` | Recommended | Label in bootstrap logs |
| `setup` | Yes | Shell script run on bootstrap |
| `dependencies` | No | Plugins to install first |
| `path_entries` | No | Directories added to VM `PATH` |
| `defaults` | No | Default `plugins.config` values |
| `agents` | No | Restrict to specific agents |

#### Config-driven installs

`setup` supports [Go templates](https://pkg.go.dev/text/template).
Values from `plugins.config.<name>` are available as template fields:

```yaml
plugins:
  enabled:
    - my-tool
  config:
    my-tool:
      version: "3.2.1"
  define:
    my-tool:
      dependencies: [system]
      setup: |
        curl -fsSL "https://example.com/my-tool-{{ .version }}.tar.gz" \
          | tar -xz -C "$HOME/.local"
```

| Template | Source |
| --- | --- |
| `.version`, `.repo`, … | `plugins.config` merged with `defaults` |
| `.state_dir` | aivm state directory (auto-injected) |
| `toYAML` | Serialize a value to YAML |
| `b64enc` | Base64-encode for safe shell embedding |

Run `aivm recreate` and verify with `aivm ssh`.

---

## Agents

Agents are the AI coding CLIs that aivm launches inside the VM. Configure them
under `agents` in `aivm.yaml`.

All agents listed in `agents.enabled` are installed during bootstrap, so you can
switch between them with `--agent` without rebuilding the VM. If only one agent
is enabled, `agents.default` is inferred automatically. Enabled agent plugins are
registered automatically based on `agents.enabled`.

Built-in agent definitions live in
[`internal/agent/defaults.yaml`](internal/agent/defaults.yaml).

### Customizing agents

| Field | Purpose |
| --- | --- |
| `cli_command` | Binary invoked in the VM |
| `launch_args` | Flags appended by bare `aivm` (not `aivm agent --`) |
| `setup` | Override the agent's install script |
| `dependencies` | Plugins/toolchains required before install |
| `path_entries` | Directories added to VM `PATH` |

```yaml
agents:
  enabled:
    - claude
  define:
    claude:
      launch_args: "--dangerously-skip-permissions --model opus"
```

### Claude Code

```yaml
agents:
  default: claude
  enabled:
    - claude
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
  enabled:
    - copilot
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
  enabled:
    - cursor
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
  enabled:
    - opencode
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

# Install to ~/.local/bin/aivm (override with AIVM_INSTALL_DIR)
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
