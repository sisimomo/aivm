package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func SSHCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh",
		Short: "Open an interactive shell in the VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoSSH(cmd.Context(), app)
		},
	}
}

func DoSSH(ctx context.Context, app *App) error {
	return app.Lifecycle.SSH(ctx)
}
