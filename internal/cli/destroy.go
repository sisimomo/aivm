package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func DestroyCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Delete the VM (volumes and host state preserved)",
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
	return app.Lifecycle.Destroy(ctx)
}
