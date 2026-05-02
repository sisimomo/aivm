package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"aivm/internal/bootstrap"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

func BootstrapCmd(app *App) *cobra.Command {
	var listOnly bool
	var forcePlugin string

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Run VM bootstrap (installs all tools)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if listOnly {
				return ListPlugins(app)
			}
			return DoBootstrap(cmd.Context(), app, forcePlugin)
		},
	}
	cmd.Flags().BoolVar(&listOnly, "list", false, "list all plugins and their status")
	cmd.Flags().StringVar(&forcePlugin, "plugin", "", "run only this specific plugin")
	return cmd
}

func DoBootstrap(ctx context.Context, app *App, onlyPlugin string) error {
	status, err := app.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	cfg := app.Config
	enabled := cfg.Plugins.Enabled
	if onlyPlugin != "" {
		enabled = []string{onlyPlugin}
	}

	eng := &bootstrap.Engine{
		VM: app.VM,
		Executor: &plugin.Executor{
			Registry:     app.Registry,
			Enabled:      enabled,
			PluginConfig: cfg.Plugins.Config,
			StateDir:     cfg.StateDir,
			VMInst:       app.VM,
		},
		StateDir: cfg.StateDir,
	}
	return eng.Run(ctx, onlyPlugin != "")
}

func ListPlugins(app *App) error {
	all := app.Registry.All()
	fmt.Printf("\n  %-16s %-40s %s\n", "NAME", "DESCRIPTION", "DEPENDS ON")
	fmt.Printf("  %-16s %-40s %s\n", "────────────────", "────────────────────────────────────────", "──────────")
	ordered, _ := app.Registry.Resolve(app.Config.Plugins.Enabled)
	shown := make(map[string]bool)
	for _, p := range ordered {
		shown[p.Name()] = true
		fmt.Printf("  %-16s %-40s %v\n", p.Name(), p.Description(), p.Dependencies())
	}
	for name, p := range all {
		if !shown[name] {
			fmt.Printf("  %-16s %-40s %v  (disabled)\n", p.Name(), p.Description(), p.Dependencies())
		}
	}
	fmt.Println()
	return nil
}
