# aivm Domain Model

This document defines key concepts used across the codebase. Consistent terminology is essential for understanding architecture decisions and reading refactored modules.

## Core Concepts

### VM Lifecycle

**VM** — The Colima virtual machine instance. May be in one of several states (not running, suspended, running). Mutable; recreated based on age or explicit user commands.

**VM Profile** — A named Colima VM profile (e.g., `aivm`, `aivm-dev`). Acts as the namespace for VM resources.

**VM Status** — One of: `NotFound`, `Stopped`, `Running`, `Suspended`.

**VM Transition** — The state change required to reach a goal (e.g., "is this a fresh VM? Did it age? Is it suspended?"). A transition can involve starting the VM, soft-rebuilding, or recreating from the base image.

### Bootstrap & Installation

**Plugin** — A piece of VM setup logic. Declares a `skip_if` script (exit 0 = already set up, skip), a `setup` script, optional dependencies, and optional configuration. Examples: `java`, `nodejs`, `rtk`, `claude`, `copilot`.

**Plugin Dependency Graph** — The set of `dependencies` declared by all plugins, resolved into a topological order so each plugin runs only after its dependencies are satisfied.

**Plugin Registry** — The registry of all available plugins. Built-in plugins are loaded from YAML defaults; user overrides are merged in from the config file.

**Bootstrap** — The process of ensuring the VM has all required tools installed. Runs the plugin DAG in order, checks each plugin's `skip_if` script for idempotency, and saves bootstrap state on success.

**Bootstrap State** — Persistent host-side record (`bootstrap-state.json`) containing the bootstrap schema version, active provider name, and a hash of all execution-relevant configuration (`config_hash`). Used for change detection: if the hash matches the current config, bootstrap is skipped entirely.

**Config Hash** — A SHA-256 digest computed over the enabled plugin list, effective plugin definitions, plugin config overrides, integration definitions, and active provider name. Stored in Bootstrap State. Any change to these inputs produces a different hash, triggering a full reconcile on next start.

**Reconcile** — Running bootstrap without forcing all plugins to reinstall (`force=false`). Each plugin's `skip_if` script determines whether it runs; already-set-up plugins are skipped.

### Integration & Agent Setup

**Agent** — An AI coding assistant runtime (e.g., Claude Code, GitHub Copilot). Agents are decoupled from plugins; each agent has a provider.

**Agent Provider** — The concrete implementation of an agent (e.g., `claude`, `copilot`). Handles authentication, launch command, and runtime configuration.

**Integration** — A rule that applies plugin configuration when a specific agent becomes active. Runs after bootstrap completes. Has a `skip_if` script for idempotency. Example: "when `rtk` is installed and `claude` is active, run `rtk init -g --auto-patch`".

**Agent Definition** — Metadata about an agent: its name, required plugin, bootstrap scripts, and MCP client config. Merges built-in defaults with user overrides from the config file.

### Configuration & Composition

**Configuration** — The merged result of: build-time defaults + YAML config file + environment variable overrides + agent-specific logic. Includes VM settings, MCP settings, idle timeouts, enabled plugins, and per-plugin config.

**Configuration Precedence** — The order in which sources override each other. From lowest to highest: build defaults → YAML → environment variables → agent-specific logic.

**Plugin Composition** — The process of assembling the effective set of plugins: load built-in definitions → merge agent definitions as plugins → merge user overrides → register in the plugin registry.

### Session & Monitoring

**Session** — A single `aivm` invocation. Sessions are tracked in lock files and used by the idle monitor to detect when the VM should suspend.

**Idle Monitor** — A background daemon that tracks active sessions and manages VM suspension/deletion when idle. Operates in two phases: suspend (after timeout), then delete (after additional timeout).

**Idle Timeout** — Duration of inactivity (no active sessions) before the VM is suspended. Default: 5m.

**Delete Timeout** — Duration of suspension before the VM is deleted entirely. Default: 5m.

### Base Image & VM Rotation

**Base Image** — A snapshot of the VM taken after a successful bootstrap. Used to speed up VM recreation: instead of bootstrapping from scratch, restore from the base image and reconcile.

**VM Age** — Time since the base image was created. Configurable threshold (`vm.recreate_prompt_after`) triggers a prompt to recreate the VM.

**Soft Rebuild** — Recreate the base image while the current VM keeps running. The current VM finishes all active sessions, then auto-deletes; the rebuild VM becomes the new base image.

**Hard Rebuild** — Kill all active sessions, destroy the current VM immediately, and rebuild the base image. Faster but disruptive.

### Execution & Operations

**Launch** — Start the active agent in the current directory inside the VM. Requires the VM to be running and bootstrapped.

**SSH** — Execute a command inside the VM via SSH. Used by launch, bootstrap, and diagnostics.

**Run** — Generic VM command execution (via SSH or other transport). Used by bootstrap plugins to execute install scripts.

## Key Relationships

- A **Configuration** determines which **Plugins** are enabled and their settings.
- A **Plugin** may depend on other **Plugins**; the dependency graph is resolved to create an execution order.
- A **Bootstrap** runs all plugins in dependency order, producing a **Bootstrap State**.
- An **Agent** is selected from the active provider in configuration.
- An **Integration** links an enabled **Plugin** to an active **Agent**, applying configuration.
- An **Idle Monitor** tracks **Sessions** and manages VM suspension/deletion.
- A **Base Image** is created after a successful **Bootstrap**; used to speed **VM** recreation.
- A **VM Transition** is the plan to move from the current VM status to a desired state (running, bootstrapped, etc.).
