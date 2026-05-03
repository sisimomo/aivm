package cli

import (
"aivm/internal/agent"
"aivm/internal/config"
"aivm/internal/lifecycle"
"aivm/internal/mcp"
"aivm/internal/monitor"
"aivm/internal/session"
"aivm/internal/vm"
)

// App is the central dependency container for the aivm CLI.
// Lifecycle owns all orchestration logic. The remaining fields are
// convenience accessors for read-only commands (status, ssh, logs).
type App struct {
Lifecycle *lifecycle.LifecycleService

// Convenience fields shared with Lifecycle for read-only commands.
Config   *config.Config
VM       vm.VM
MCP      mcp.MCPManager
Sessions *session.Store
Monitor  *monitor.IdleMonitor
Agents   *agent.Registry
}
