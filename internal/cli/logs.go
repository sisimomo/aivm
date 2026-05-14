package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func LogsCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "logs <service>",
		Short: "Show logs (monitor | bootstrap | vm | <sidecar-name>)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoLogs(cmd.Context(), app, args[0])
		},
	}
}

func DoLogs(_ context.Context, app *App, service string) error {
	return app.Lifecycle.Logs(service)
}
