package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/cli"
	"github.com/sisimomo/aivm/internal/compose"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/monitor"
	"github.com/sisimomo/aivm/internal/providers/generic"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/t3code"
	"github.com/sisimomo/aivm/internal/vm"
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
		StateDir: defaultStateDir,
	}

	// Load built-in agent definitions and auto-register a generic provider for each.
	// To add a new agent, add an entry to internal/agent/defaults.yaml — no Go code needed.
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		return nil, fmt.Errorf("loading agent definitions: %w", err)
	}
	agentReg := agent.NewRegistry()
	for name, def := range agentDefs {
		agentReg.Register(generic.NewFromDef(name, def))
	}

	// Compose the full configuration: load config, merge agents, merge plugins,
	// build the registry, and load integrations.
	engine := &config.CompositionEngine{Defaults: d}
	compResult, err := engine.Compose(cfgPath, agentReg)
	if err != nil {
		return nil, err
	}

	cfg := compResult.Config

	// Wire config-level debug flag so `debug: true` in aivm.yaml behaves
	// identically to the --debug CLI flag.
	if cfg.Debug {
		aivmlog.SetDebug(true)
	}

	vmInst, err := vm.NewFromConfig(&cfg.VM, cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("vm backend: %w", err)
	}

	dockerHost := ""
	if cfg.ComposeFile != "" {
		dockerHostProbe, err := compose.FindHostDockerSocket(context.Background(), cfg.VM.Profile())
		if err != nil {
			aivmlog.Warn("Docker socket: %v", err)
		} else {
			dockerHost = dockerHostProbe
		}
	}

	composeMgr := &compose.Manager{
		ComposeFile: cfg.ComposeFile,
		DockerHost:  dockerHost,
		Profile:     vmInst.Profile(),
	}

	sessions := session.NewStore(cfg.StateDir)
	mon := monitor.NewIdleMonitor(sessions, vmInst, composeMgr, cfg.Idle.StopTimeout, cfg.Idle.DeleteTimeout, cfg.StateDir)
	if cfg.Idle.PollInterval > 0 {
		mon.PollInterval = cfg.Idle.PollInterval
	}

	var t3codeMgr t3code.Manager
	if cfg.T3Code.Enable {
		// Backends that need port binding at boot (e.g. Docker) expose the port
		// via PortMappings in StartOptions — no tunnel required. Colima uses an
		// SSH tunnel to forward the port from the VM to the host.
		if !vmInst.NeedsPortBindingAtBoot() {
			t3codeMgr = &t3code.Tunnel{
				Profile:  cfg.VM.Profile(),
				StateDir: cfg.StateDir,
			}
		} else {
			t3codeMgr = &t3code.NoopManager{}
		}
	} else {
		t3codeMgr = &t3code.NoopManager{}
	}

	return &cli.App{
		Lifecycle: &lifecycle.LifecycleService{
			Config:       cfg,
			VM:           vmInst,
			Compose:      composeMgr,
			T3Code:       t3codeMgr,
			Sessions:     sessions,
			Monitor:      mon,
			Registry:     compResult.Plugins,
			Agents:       compResult.Agents,
			Provider:     compResult.ActiveProvider,
			AgentDefs:    map[string]agent.Def{compResult.ActiveProvider.Name(): compResult.ActiveAgentDef},
			PluginDefs:   compResult.PluginDefs,
			Integrations: compResult.Integrations,
			Confirmer:    lifecycle.NewTTYConfirmer(),
		},
	}, nil
}
