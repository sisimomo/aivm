package cli

import (
"context"

"github.com/spf13/cobra"
)

func BootstrapCmd(getApp func() (*App, error)) *cobra.Command {
var listOnly bool
var forcePlugin string
var force bool

cmd := &cobra.Command{
Use:   "bootstrap",
Short: "Run VM bootstrap (installs all tools)",
RunE: func(cmd *cobra.Command, args []string) error {
app, err := getApp()
if err != nil {
return err
}
if listOnly {
return ListPlugins(app)
}
return DoBootstrap(cmd.Context(), app, forcePlugin, force || forcePlugin != "")
},
}
cmd.Flags().BoolVar(&listOnly, "list", false, "list all plugins and their status")
cmd.Flags().StringVar(&forcePlugin, "plugin", "", "run only this specific plugin")
cmd.Flags().BoolVar(&force, "force", false, "force re-run even if already bootstrapped")
return cmd
}

func DoBootstrap(ctx context.Context, app *App, onlyPlugin string, force bool) error {
return app.Lifecycle.Bootstrap(ctx, onlyPlugin, force)
}

func ListPlugins(app *App) error {
	return app.Lifecycle.ListPlugins()
}
