package cli

import (
	"github.com/spf13/cobra"
)

func RestartCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart VM and services",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if err := DoStop(ctx, app); err != nil {
				return err
			}
			return DoStart(ctx, app)
		},
	}
}
