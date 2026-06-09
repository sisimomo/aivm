package cli

import (
	"fmt"
	"sync"

	"github.com/spf13/cobra"
)

// AppFactory constructs an App from a config file path.
// In production this loads real infrastructure; in tests it returns a pre-built mock App.
type AppFactory func(cfgPath string) (*App, error)

// NewRootCmd builds the complete Cobra command tree for aivm.
// buildApp is called lazily on the first command execution; the result is
// cached so every subcommand in a single invocation shares one App instance.
// version is printed by the "version" subcommand.
func NewRootCmd(version string, buildApp AppFactory) *cobra.Command {
	var cfgPath string
	var logLevel string
	var agentOverride string

	// Build the App exactly once per CLI invocation.
	var (
		once     sync.Once
		builtApp *App
		buildErr error
	)
	getApp := func() (*App, error) {
		once.Do(func() {
			builtApp, buildErr = buildApp(cfgPath)
		})
		return builtApp, buildErr
	}

	root := &cobra.Command{
		Use:   "aivm [directory]",
		Short: "Launch AI agents in a secure Lima VM",
		Long: `aivm — AI VM manager

Launch AI agents in a secure, disposable Lima VM.
Run from any directory under your dev root.

Built-in providers: claude, copilot, cursor, opencode

Examples:
  aivm                   Launch the configured AI agent in the current directory (starts VM if needed)
  aivm --agent copilot   Launch a specific enabled agent instead of the default
  aivm agent -- -p "…"   Run the agent CLI with custom arguments (no launch_args)
  aivm ssh               Open a shell in the VM (starts VM if needed)
  aivm start             Start VM and services
  aivm stop              Stop everything (disk preserved)
  aivm recreate          Destroy and recreate the VM from scratch
  aivm status            Show status
  aivm cp vm:/path ./    Copy a file from the VM to the host (use vm: prefix for VM paths)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			recordLogLevelFlag(cmd)
			recordAgentFlag(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if err := DoStart(ctx, app); err != nil {
				return err
			}
			return DoLaunch(ctx, app, effectiveAgent())
		},
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to aivm.yaml")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "", "log level: trace, debug, info, warn, or error (overrides AIVM_LOG_LEVEL and config)")
	root.PersistentFlags().StringVar(&agentOverride, "agent", "", "agent to use (must be enabled in config; overrides agents.default)")

	root.AddCommand(
		StartCmd(getApp),
		StopCmd(getApp),
		DestroyCmd(getApp),
		RestartCmd(getApp),
		StatusCmd(getApp),
		AgentCmd(getApp, &agentOverride),
		SSHCmd(getApp),
		CpCmd(getApp),
		RecreateCmd(getApp),
		LogsCmd(getApp),
		monitorCmd(getApp),
		&cobra.Command{
			Use:   "version",
			Short: "Print version",
			Run:   func(_ *cobra.Command, _ []string) { fmt.Println("aivm " + version) },
		},
	)

	return root
}

// monitorCmd is the internal daemon command that runs the idle monitor.
// It is hidden from help output and intended for fork-exec by the monitor package.
func monitorCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:    "__monitor",
		Short:  "Internal: run idle monitor daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return app.Lifecycle.RunMonitor(cmd.Context())
		},
	}
}
