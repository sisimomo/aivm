package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func StartCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start VM and services",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoStart(cmd.Context(), app)
		},
	}
}

func DoStart(ctx context.Context, app *App) error {
	return app.Lifecycle.Start(ctx)
}
