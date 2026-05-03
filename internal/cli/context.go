package cli

import (
	"io"

	"aivm/internal/agent"
	"aivm/internal/config"
	"aivm/internal/integration"
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
	// AgentDefs is the effective set of agent definitions, merging built-in
	// defaults with any user overrides from agents.define in aivm.yaml.
	// Used by DoLaunch to pass runtime defaults (e.g. launch_command) to the provider.
	AgentDefs map[string]agent.Def
	// Provider is the active AI agent provider selected from the config.
	Provider agent.Provider
	// Integrations is the complete list of integrations to evaluate during
	// bootstrap. It combines built-in defaults with any user-defined integrations
	// from aivm.yaml. Tests substitute lightweight stub scripts.
	Integrations []integration.IntegrationDef
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
	// GetWorkDir returns the working directory used by DoLaunch for path resolution.
	// When nil, os.Getwd() is used. Override in tests to decouple from the real CWD.
	GetWorkDir func() (string, error)
}
