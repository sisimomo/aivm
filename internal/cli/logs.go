package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func LogsCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "logs [component]",
		Short: "Tail log files (aivm | monitor)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			service := ""
			if len(args) > 0 {
				service = args[0]
			}
			return DoLogs(cmd.Context(), app, service)
		},
	}
}

func DoLogs(_ context.Context, app *App, service string) error {
	return app.Lifecycle.Logs(service)
}
