package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

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

	var cfgPath string
	var debugMode bool

	root := &cobra.Command{
		Use:   "aivm [directory]",
		Short: "Launch AI agents in a secure Colima VM",
		Long: `aivm — AI VM manager

Launch AI agents in a secure, disposable Colima VM.
Run from any directory under your dev root.

Supported providers: claude, copilot

Examples:
  aivm                   Launch the configured AI agent in the current directory
  aivm start             Start VM and services
  aivm stop              Stop everything (disk preserved)
  aivm status            Show status
  aivm ssh               Open a shell in the VM`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if debugMode {
				aivmlog.SetDebug(true)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(cfgPath)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if err := cli.DoStart(ctx, app); err != nil {
				return err
			}
			return cli.DoLaunch(ctx, app)
		},
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to aivm.yaml")
	root.PersistentFlags().BoolVar(&debugMode, "debug", false, "enable debug logging")

	root.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start VM and services",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				return cli.DoStart(cmd.Context(), app)
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop VM and services",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				return cli.DoStop(cmd.Context(), app)
			},
		},
		&cobra.Command{
			Use:   "destroy",
			Short: "Delete the VM",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				return cli.DoDestroy(cmd.Context(), app)
			},
		},
		&cobra.Command{
			Use:   "restart",
			Short: "Restart VM and services",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				if err := cli.DoStop(cmd.Context(), app); err != nil {
					return err
				}
				return cli.DoStart(cmd.Context(), app)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show status",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				return cli.DoStatus(cmd.Context(), app)
			},
		},
		&cobra.Command{
			Use:   "ssh",
			Short: "Open shell in VM",
			RunE: func(cmd *cobra.Command, args []string) error {
				app, err := buildApp(cfgPath)
				if err != nil {
					return err
				}
				return cli.DoSSH(cmd.Context(), app)
			},
		},
		bootstrapSubcmd(&cfgPath),
		rebuildImageSubcmd(&cfgPath),
		logsSubcmd(&cfgPath),
		monitorSubcmd(&cfgPath),
		legacyMonitorSubcmd(&cfgPath),
		&cobra.Command{
			Use:   "version",
			Short: "Print version",
			Run:   func(_ *cobra.Command, _ []string) { fmt.Println("aivm " + version) },
		},
	)

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

	composeFile, err := mcp.EnsureComposeFile(cfg.StateDir)
	if err != nil {
		return nil, fmt.Errorf("preparing compose file: %w", err)
	}

	mcpMgr := &mcp.Manager{
		ComposeFile:   composeFile,
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

func bootstrapSubcmd(cfgPath *string) *cobra.Command {
	var listOnly bool
	var forcePlugin string
	var force bool
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Run VM bootstrap",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(*cfgPath)
			if err != nil {
				return err
			}
			if listOnly {
				return cli.ListPlugins(app)
			}
			return cli.DoBootstrap(cmd.Context(), app, forcePlugin, force || forcePlugin != "")
		},
	}
	cmd.Flags().BoolVar(&listOnly, "list", false, "list plugins")
	cmd.Flags().StringVar(&forcePlugin, "plugin", "", "run only this plugin")
	cmd.Flags().BoolVar(&force, "force", false, "force re-run even if already bootstrapped")
	return cmd
}

func rebuildImageSubcmd(cfgPath *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rebuild-image",
		Short: "Rebuild the base VM image by re-running bootstrap",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(*cfgPath)
			if err != nil {
				return err
			}
			return cli.DoRebuildImage(cmd.Context(), app, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

func logsSubcmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "logs [service]",
		Short: "Show logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(*cfgPath)
			if err != nil {
				return err
			}
			svc := "mcpjungle"
			if len(args) > 0 {
				svc = args[0]
			}
			return cli.DoLogs(cmd.Context(), app, svc)
		},
	}
}

func monitorSubcmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "__monitor",
		Short:  "Internal: run idle monitor daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(*cfgPath)
			if err != nil {
				return err
			}
			return app.Monitor.Run(cmd.Context())
		},
	}
}

func legacyMonitorSubcmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "__legacy-monitor",
		Short:  "Internal: destroy legacy VM once its sessions drain",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := buildApp(*cfgPath)
			if err != nil {
				return err
			}
			ts := vm.LoadTransitionState(app.Config.StateDir)
			if ts == nil {
				return nil // no transition in progress
			}
			return app.Monitor.RunLegacyMonitor(cmd.Context(), ts)
		},
	}
}
