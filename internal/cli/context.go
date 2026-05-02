package cli

import (
	"io"

	"aivm/internal/agent"
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
	MCP      mcp.MCPManager
	Sessions *session.Store
	Monitor  *monitor.IdleMonitor
	Registry *plugin.Registry
	Agents   *agent.Registry
	// Provider is the active AI agent provider selected from the config.
	Provider agent.Provider
	// VMFactory creates VM instances for profiles other than the primary VM
	// (e.g. the temporary rebuild VM in doSoftRebuild). In production this is
	// vm.NewColima; tests substitute a mock factory via the test harness.
	VMFactory vm.VMFactory

	// Stdin is the reader used for interactive prompt answers.
	// Defaults to os.Stdin when nil.
	Stdin io.Reader
	// IsTerminal reports whether the process is attached to an interactive terminal.
	// Defaults to the real os.Stdin character-device check when nil.
	IsTerminal func() bool
}
