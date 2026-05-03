package cli

import (
"context"
"fmt"

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
svc := app.Lifecycle
all := svc.Registry.All()
fmt.Printf("\n  %-16s %-40s %s\n", "NAME", "DESCRIPTION", "DEPENDS ON")
fmt.Printf("  %-16s %-40s %s\n", "────────────────", "────────────────────────────────────────", "──────────")
ordered, _ := svc.Registry.Resolve(svc.Config.Plugins.Enabled)
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
