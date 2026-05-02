package cli

import (
	"context"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
)

func DestroyCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Delete the VM (host state preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoDestroy(cmd.Context(), app)
		},
	}
}

func DoDestroy(ctx context.Context, app *App) error {
	app.Monitor.Stop()
	if err := app.VM.Destroy(ctx); err != nil {
		return err
	}
	aivmlog.Success("VM destroyed")
	return nil
}
