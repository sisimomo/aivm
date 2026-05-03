package main

import (
	"context"
	"os"

	"aivm/internal/agent"
	"aivm/internal/cli"
	"aivm/internal/config"
	aivmlog "aivm/internal/log"
	"aivm/internal/lifecycle"
	"aivm/internal/mcp"
	"aivm/internal/monitor"
	"aivm/internal/providers/claude"
	"aivm/internal/providers/copilot"
	"aivm/internal/session"
	"aivm/internal/vm"
)

var version = "dev"

// Build-time injectable defaults. Override via:
//
//	-ldflags "-X main.defaultStateDir=~/.aivm-dev"
var (
	defaultStateDir = "~/.aivm"
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
	d := config.Defaults{
		StateDir:  defaultStateDir,
	}

	// Build the agent provider registry.
	agentReg := agent.NewRegistry()
	agentReg.Register(claude.New())
	agentReg.Register(copilot.New())

	// Compose the full configuration: load config, merge agents, merge plugins,
	// build the registry, and load integrations.
	engine := &config.CompositionEngine{Defaults: d}
	compResult, err := engine.Compose(cfgPath, agentReg)
	if err != nil {
		return nil, err
	}

	cfg := compResult.Config
	vmInst := vm.NewColima(cfg.VM.ColimaProfile, cfg.StateDir)
	dockerHost, err := mcp.FindHostDockerSocket(context.Background(), cfg.VM.ColimaProfile)
	if err != nil {
		aivmlog.Warn("Docker socket: %v", err)
		dockerHost = ""
	}

	devRoot := ""
	for _, m := range cfg.VM.ParsedMounts {
		if m.Writable {
			devRoot = m.HostPath
			break
		}
	}

	mcpMgr := &mcp.Manager{
		Port:          cfg.MCP.Port,
		DataDir:       cfg.MCP.DataDir,
		DockerHost:    dockerHost,
		DevRoot:       devRoot,
		ImageTag:      cfg.MCP.ImageTag,
		ServerMode:    cfg.MCP.ServerMode,
		ContainerName: "mcpjungle-" + cfg.VM.ColimaProfile,
	}

	sessions := session.NewStore(cfg.StateDir)
	mon := monitor.NewIdleMonitor(sessions, vmInst, mcpMgr, cfg.Idle.StopTimeout, cfg.Idle.DeleteTimeout, cfg.StateDir)
	mon.VMFactory = vm.ColimaFactory

	return &cli.App{
		Lifecycle: &lifecycle.LifecycleService{
			Config:       cfg,
			VM:           vmInst,
			MCP:          mcpMgr,
			Sessions:     sessions,
			Monitor:      mon,
			Registry:     compResult.Plugins,
			Agents:       compResult.Agents,
			Provider:     compResult.ActiveProvider,
			AgentDefs:    map[string]agent.Def{compResult.ActiveProvider.Name(): compResult.ActiveAgentDef},
			PluginDefs:   compResult.PluginDefs,
			VMFactory:    vm.ColimaFactory,
			Integrations: compResult.Integrations,
			Confirmer:    lifecycle.NewTTYConfirmer(),
		},
	}, nil
}