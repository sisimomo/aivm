package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"aivm/internal/agent"
	"aivm/internal/cli"
	"aivm/internal/config"
	aivmlog "aivm/internal/log"
	"aivm/internal/mcp"
	"aivm/internal/monitor"
	"aivm/internal/plugin"
	"aivm/internal/providers/claude"
	"aivm/internal/providers/copilot"
	"aivm/internal/session"
	"aivm/internal/vm"
)

var version = "dev"

// Build-time injectable defaults. Override via:
//
//	-ldflags "-X main.defaultStateDir=~/.aivm-dev -X main.defaultProfile=aivm-dev -X main.defaultMCPPort=7594"
var (
	defaultStateDir = "~/.aivm"
	defaultProfile  = "aivm"
	defaultMCPPort  = "7593"
)

func main() {
	os.Setenv("PATH", "/opt/homebrew/bin:/usr/local/bin:"+os.Getenv("PATH"))

	root := cli.NewRootCmd(version, buildApp)
	if err := root.ExecuteContext(context.Background()); err != nil {
		aivmlog.Error("%v", err)
		os.Exit(1)
	}
}

func buildApp(cfgPath string) (*cli.App, error) {
	port, _ := strconv.Atoi(defaultMCPPort)
	if port == 0 {
		port = 7593
	}
	d := config.Defaults{
		StateDir:  defaultStateDir,
		VMProfile: defaultProfile,
		MCPPort:   port,
	}
	cfg, err := config.Load(cfgPath, d)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	// Build the agent provider registry and select the active provider.
	agentReg := agent.NewRegistry()
	agentReg.Register(claude.New())
	agentReg.Register(copilot.New())

	providerName := cfg.Agent.Provider
	prov, ok := agentReg.Get(providerName)
	if !ok {
		return nil, fmt.Errorf("unknown agent provider %q — supported: claude, copilot", providerName)
	}

	vmInst := vm.NewColima(cfg.VM.Profile, cfg.StateDir)
	dockerHost, err := mcp.FindHostDockerSocket(context.Background(), cfg.VM.Profile)
	if err != nil {
		aivmlog.Warn("Docker socket: %v", err)
		dockerHost = ""
	}

	mcpMgr := &mcp.Manager{
		Port:          cfg.MCP.Port,
		DataDir:       cfg.MCP.DataDir,
		DockerHost:    dockerHost,
		DevRoot:       cfg.VM.DevRoot,
		ImageTag:      cfg.MCP.ImageTag,
		ServerMode:    cfg.MCP.ServerMode,
		ContainerName: "mcpjungle-" + cfg.VM.Profile,
	}

	sessions := session.NewStore(cfg.StateDir)
	mon := monitor.NewIdleMonitor(sessions, vmInst, mcpMgr, cfg.Idle.Timeout, cfg.Idle.DeleteTimeout, cfg.StateDir)
	mon.VMFactory = vm.ColimaFactory

	reg := plugin.Global()

	// Load bundled plugin definitions, then merge any user overrides from plugins.define.
	defs, err := plugin.LoadDefaults()
	if err != nil {
		return nil, fmt.Errorf("loading plugin defaults: %w", err)
	}
	for name, override := range cfg.Plugins.Define {
		base := defs[name] // zero value if not in defaults (new plugin)
		defs[name] = plugin.MergePluginDef(base, override)
	}
	for name, def := range defs {
		reg.Set(plugin.NewYAMLPlugin(name, def))
	}

	return &cli.App{
		Config:    cfg,
		VM:        vmInst,
		MCP:       mcpMgr,
		Sessions:  sessions,
		Monitor:   mon,
		Registry:  reg,
		Agents:    agentReg,
		Provider:  prov,
		VMFactory: vm.ColimaFactory,
	}, nil
}


