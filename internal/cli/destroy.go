package cli

import (
	"context"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
)

func DestroyCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Delete the VM (host state preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoDestroy(cmd.Context(), app)
		},
	}
}

func DoDestroy(ctx context.Context, app *App) error {
	app.Monitor.Stop()
	if err := app.VM.Destroy(ctx); err != nil {
		return err
	}
	if err := app.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("VM destroyed")
	return nil
}
