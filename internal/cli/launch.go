package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func LaunchCmd(getApp func() (*App, error), agentOverride *string) *cobra.Command {
	return &cobra.Command{
		Use:   "launch [directory]",
		Short: "Launch the configured AI agent in the VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoLaunch(cmd.Context(), app, *agentOverride)
		},
	}
}

func DoLaunch(ctx context.Context, app *App, agentOverride string) error {
	return app.Lifecycle.Launch(ctx, agentOverride)
}
