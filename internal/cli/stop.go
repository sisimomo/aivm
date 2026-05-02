package cli

import (
	"context"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
)

func StopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop VM and services (disk preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoStop(cmd.Context(), app)
		},
	}
}

func DoStop(ctx context.Context, app *App) error {
	aivmlog.Step("Stopping aivm")
	app.Monitor.Stop()
	if err := app.VM.Stop(ctx); err != nil {
		aivmlog.Warn("VM stop error: %v", err)
	}
	if err := app.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("aivm stopped")
	return nil
}
