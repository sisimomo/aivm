package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"

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
		slog.Error(fmt.Sprintf("%v", err))
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 1
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

	for name, def := range compResult.CustomAgentDefs {
		agentReg.Register(generic.NewFromDef(name, def))
	}
	activeProv := compResult.ActiveProvider
	if activeProv == nil {
		activeProv, _ = agentReg.Get(compResult.DefaultAgent)
	}

	cfg := compResult.Config

	if err := cli.ApplyLogLevel(cfg.LogLevel); err != nil {
		return nil, err
	}
	if err := aivmlog.InitStateDir(cfg.StateDir); err != nil {
		return nil, fmt.Errorf("initializing logs: %w", err)
	}

	vmInst, err := vm.NewFromConfig(&cfg.VM, cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("vm backend: %w", err)
	}

	dockerHost := ""
	if cfg.ComposeFile != "" {
		dockerHostProbe, err := compose.FindHostDockerSocket(context.Background(), cfg.VM.Profile())
		if err != nil {
			slog.Warn(fmt.Sprintf("Docker socket: %v", err))
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

	// Sort names for deterministic bootstrap plugin order.
	enabledNames := make([]string, 0, len(compResult.EnabledAgentDefs))
	for name := range compResult.EnabledAgentDefs {
		enabledNames = append(enabledNames, name)
	}
	sort.Strings(enabledNames)
	enabledProviders := make([]agent.Provider, 0, len(enabledNames))
	for _, name := range enabledNames {
		if p, ok := agentReg.Get(name); ok {
			enabledProviders = append(enabledProviders, p)
		}
	}

	return &cli.App{
		Lifecycle: &lifecycle.LifecycleService{
			Config:           cfg,
			VM:               vmInst,
			Compose:          composeMgr,
			T3Code:           t3codeMgr,
			Sessions:         sessions,
			Monitor:          mon,
			Registry:         compResult.Plugins,
			Agents:           compResult.Agents,
			Provider:         activeProv,
			EnabledProviders: enabledProviders,
			AgentDefs:        compResult.EnabledAgentDefs,
			PluginDefs:       compResult.PluginDefs,
			Integrations:     compResult.Integrations,
			Confirmer:        lifecycle.NewTTYConfirmer(),
		},
	}, nil
}
