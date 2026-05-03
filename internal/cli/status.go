package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func StatusCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show VM and service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoStatus(cmd.Context(), app)
		},
	}
}

func DoStatus(ctx context.Context, app *App) error {
	return app.Lifecycle.Status(ctx)
}
