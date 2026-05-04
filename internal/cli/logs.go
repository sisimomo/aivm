package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func LogsCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "logs [service]",
		Short: "Show logs (mcpjungle | monitor | bootstrap | colima)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			svc := "mcpjungle"
			if len(args) > 0 {
				svc = args[0]
			}
			return DoLogs(cmd.Context(), app, svc)
		},
	}
}

func DoLogs(_ context.Context, app *App, service string) error {
	return app.Lifecycle.Logs(service)
}
