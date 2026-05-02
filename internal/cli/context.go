package cli

import (
	"aivm/internal/config"
	"aivm/internal/mcp"
	"aivm/internal/monitor"
	"aivm/internal/plugin"
	"aivm/internal/session"
	"aivm/internal/vm"
)

type App struct {
	Config   *config.Config
	VM       vm.VM
	MCP      *mcp.Manager
	Sessions *session.Store
	Monitor  *monitor.IdleMonitor
	Registry *plugin.Registry
}
