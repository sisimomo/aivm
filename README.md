# ai-vm

A production-ready local AI sandbox that runs **Claude Code** agents inside a secure, disposable [Colima](https://github.com/abiosoft/colima) VM. A single `aivm` command handles the full lifecycle — from boot to teardown.

---

## System Overview

```
HOST (macOS)                          VM (Colima: aivm)
────────────────────────────────────  ─────────────────────────────────────────
aivm CLI                              Docker Engine
MCPJungle (Docker Compose + SQLite)   Java 25 + Maven 3.x
Idle Monitor daemon                   Node.js + Claude Code
MCP Gateway (port 8080)               Client → host.lima.internal:8080/mcp
```

The **VM is persistent by default** — `aivm stop` preserves the disk for fast resume; `aivm start` picks up where you left off. State lives on the host. The VM is automatically offered for recreation when it exceeds `AIVM_VM_MAX_AGE_DAYS` (default: 7 days).

---

## Host vs VM Separation

| Concern | Host | VM |
|---|---|---|
| MCPJungle service | ✅ | ❌ |
| MCP server registry | ✅ | ❌ |
| API keys at rest | ✅ (`.env`) | ❌ |
| Java / Maven / Node | ❌ | ✅ |
| Docker workloads | ❌ | ✅ |
| Claude Code process | ❌ | ✅ |
| Git credentials | ❌ | ❌ (none) |
| SSH keys | ❌ | ❌ (none) |

---

## MCPJungle Architecture

MCPJungle acts as a centralised MCP gateway. All Claude Code sessions share a single MCPJungle instance running on the host.

```
Claude Code (VM)
  │  http://host.lima.internal:8080/mcp
  ▼
MCPJungle Gateway (host Docker, port 8080)
  │  Streamable HTTP transport (/mcp endpoint)
  │  SQLite database at ~/.aivm/mcpjungle-data/mcpjungle.db
  ├── Registered MCP Server A (stdio or HTTP)
  ├── Registered MCP Server B
  └── ...
```

MCPJungle starts when the VM starts and stops when the VM is idle for 5 minutes.

**Registering MCP servers** (from host, after `aivm start`):
```bash
mcpjungle register --name context7 \
  --url https://mcp.context7.com/mcp \
  --description "Context7 documentation"
```

---

## Requirements

| Tool | Install |
|---|---|
| [Colima](https://github.com/abiosoft/colima) | `brew install colima` |
| Docker runtime | Docker Desktop, OrbStack, or `colima start` (default profile) |
| `curl`| `brew install curl` |

> **Note:** MCPJungle needs a Docker runtime that is *separate* from the `aivm` Colima VM. If you only have Colima, run `colima start` (default profile) before `aivm`.

---

## Setup

```bash
# 1. Clone into ~/dev
git clone <repo-url> ~/dev/ai-vm
cd ~/dev/ai-vm

# 2. Install
./install.sh

# 3. Configure
cp .env.example .env
# Edit .env as needed

# 4. Launch (from any project directory under ~/dev)
cd ~/dev/my-project
aivm
```

---

## Usage

```bash
# Launch Claude Code in the current directory (starts VM automatically)
aivm

# Start VM and services only (no Claude Code session)
aivm start

# Show status
aivm status

# Open a shell in the VM
aivm ssh

# Stop everything (disk preserved for fast resume)
aivm stop

# Delete VM (host-side state like MCPJungle data is preserved)
aivm destroy

# Restart VM and services
aivm restart

# Re-run bootstrap (escape hatch — normally runs once on VM creation)
aivm bootstrap

# View logs
aivm logs mcpjungle      # MCPJungle container logs
aivm logs monitor        # Idle monitor log
aivm logs bootstrap      # Bootstrap log (from inside VM)
aivm logs colima         # Colima VM log

# Help
aivm help
```

---

## Path Mapping

`aivm` automatically translates your current directory from host to VM.

| Host | VM |
|---|---|
| `~/dev/projects/app/backend` | `/Users/simon/dev/projects/app/backend` (same path) |

**How it works:**
Lima/Colima mounts `~/dev` at the **same absolute path** inside the VM. No translation is needed — `aivm` validates that your current directory is under `~/dev` and passes it through unchanged.

**Requirement:** You must run `aivm` from inside `~/dev`. Paths outside `~/dev` are not supported and will produce an error.

---

## VM Toolchain

Installed by `bootstrap/bootstrap.sh` on first launch:

| Tool | Version |
|---|---|
| Java | 25 (via SDKMAN) |
| Maven | Latest 3.x (via SDKMAN) |
| Node.js | Latest LTS (via fnm) |
| Claude Code | Latest |
| Docker | Pre-installed by Colima |
| Git | System package |

---

## VM Configuration

Defaults (override in `.env`):

| Variable | Default | Description |
|---|---|---|
| `AIVM_VM_CPUS` | `4` | VM CPU count |
| `AIVM_VM_MEMORY` | `8` | VM RAM (GiB) |
| `AIVM_VM_DISK` | `60` | VM disk (GiB) |
| `AIVM_VM_TYPE` | `vz` | `vz` (Apple Silicon) or `qemu` |
| `AIVM_VM_MAX_AGE_DAYS` | `7` | Days before prompting to recreate VM |
| `MCPJUNGLE_PORT` | `8080` | MCPJungle port on host |
| `AIVM_IDLE_TIMEOUT` | `300` | Idle shutdown after N seconds |

---

## Security Model

- **No SSH keys** inside the VM
- **No git credentials** inside the VM
- **MCPJungle** is the only external integration surface; it runs on the host and is not accessible from outside `127.0.0.1`
- The VM is **easily disposable** — `aivm destroy` deletes it; `aivm` rebuilds from scratch
- MCPJungle data (SQLite) persists at `~/.aivm/mcpjungle-data/` on the host

---

## Idle Monitor

`scripts/lifecycle/idle-monitor.sh` runs as a background daemon on the host.

- Polls every 30 seconds for active session lock files
- A session is active if its PID is alive (verified with PID + start-time)
- After `AIVM_IDLE_TIMEOUT` seconds (default: 5 min) with no active sessions:
  1. Stops Docker containers inside VM
  2. Stops Colima VM (disk preserved for fast resume)
  3. Stops MCPJungle

Handles: killed terminals, orphaned processes, stale lock files.

---

## Multi-Instance Support

Multiple `aivm` invocations can run **simultaneously** in the same VM. Each session:
- Gets its own SSH connection to the VM
- Gets its own `claude` process
- Tracks its own session lock file

All sessions share the same VM, Docker daemon, and MCPJungle instance.

---

## Reset / Clean Slate

`aivm stop` preserves the VM disk. To delete the VM and start fresh:

```bash
# Delete the VM — host-side state (MCPJungle data, sessions, logs) is preserved
aivm destroy

# Restart with a clean VM; bootstrap runs automatically
aivm
```

> **Tip:** If the VM is older than `AIVM_VM_MAX_AGE_DAYS` days (default: 7), `aivm start` will prompt you interactively to delete and recreate it.

---

## Troubleshooting

**`aivm` says "No suitable host Docker runtime found"**
> MCPJungle needs its own Docker runtime. Install Docker Desktop, OrbStack, or run `colima start` (default profile).

**Claude Code can't reach MCPJungle**
> Verify: `curl http://host.lima.internal:8080/health` from inside the VM (`aivm ssh`). If it fails, check that MCPJungle is healthy: `aivm status`.

**Bootstrap fails on Java 25**
> SDKMAN occasionally uses different version identifiers. Run `aivm ssh` then `sdk list java | grep 25` to find the exact version string.

**Idle monitor doesn't stop the VM**
> Check the monitor log: `aivm logs monitor`. Ensure no stale lock files remain in `~/.aivm/sessions/`.

---

## Repository Structure

```
ai-vm/
├── bin/
│   └── aivm                       ← CLI entry point (add to PATH via install.sh)
├── bootstrap/
│   └── bootstrap.sh               ← VM bootstrap (Java, Maven, Node, Claude Code)
├── scripts/
│   ├── lifecycle/
│   │   ├── idle-monitor.sh        ← Host-side idle shutdown daemon
│   │   ├── vm-start.sh            ← Start Colima VM
│   │   ├── vm-stop.sh             ← Stop Colima VM (disk preserved)
│   │   └── vm-destroy.sh          ← Delete VM and wipe all VM-side state
│   ├── mcp/
│   │   ├── start-mcpjungle.sh     ← Start MCPJungle (host Docker)
│   │   └── stop-mcpjungle.sh      ← Stop MCPJungle
│   └── utils/
│       └── logging.sh             ← Shared logging utilities
├── config/
│   ├── colima.yaml                ← Colima VM template reference
│   └── mcp-client-config.json    ← MCP config template for Claude Code
├── docker-compose.mcpjungle.yml   ← MCPJungle service (SQLite, host-side)
├── .env.example                   ← Configuration template
├── install.sh                     ← One-time installer
└── README.md
```
