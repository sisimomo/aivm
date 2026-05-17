package cli

import (
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	aivmlog "github.com/sisimomo/aivm/internal/log"
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
	var debugMode bool

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
		Short: "Launch AI agents in a secure Colima VM",
		Long: `aivm — AI VM manager

Launch AI agents in a secure, disposable Colima VM.
Run from any directory under your dev root.

Supported providers: claude, copilot

Examples:
  aivm                   Launch the configured AI agent in the current directory (starts VM if needed)
  aivm ssh               Open a shell in the VM (starts VM if needed)
  aivm start             Start VM and services
  aivm stop              Stop everything (disk preserved)
  aivm status            Show status
  aivm recreate          Restore VM from base image (clean environment)
  aivm recreate --rebuild  Full bootstrap and save a new base image
  aivm rebuild-image     Rebuild base image using shadow VM (running VM untouched)`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if debugMode {
				aivmlog.SetDebug(true)
			}
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
			return DoLaunch(ctx, app)
		},
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "path to aivm.yaml")
	root.PersistentFlags().BoolVar(&debugMode, "debug", false, "enable debug logging")

	root.AddCommand(
		StartCmd(getApp),
		StopCmd(getApp),
		DestroyCmd(getApp),
		RestartCmd(getApp),
		StatusCmd(getApp),
		SSHCmd(getApp),
		RecreateCmd(getApp),
		RebuildImageCmd(getApp),
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
