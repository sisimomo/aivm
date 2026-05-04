package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func StopCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop VM and services (disk preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoStop(cmd.Context(), app)
		},
	}
}

func DoStop(ctx context.Context, app *App) error {
	return app.Lifecycle.Stop(ctx)
}
