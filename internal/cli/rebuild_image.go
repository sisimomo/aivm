package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func RebuildImageCmd(getApp func() (*App, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebuild-image",
		Short: "Rebuild the base VM image without interrupting the running VM",
		Long: `Rebuild the base VM image using a shadow VM.

A temporary VM is created in the background, the full bootstrap process runs
on it unconditionally from a clean slate, and the resulting snapshot is saved
as the new base image. The shadow VM is then destroyed automatically.

The currently running VM and any active sessions are not interrupted.

After rebuild-image completes, run 'aivm recreate' to apply the new base image
to the running VM.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoRebuildImage(cmd.Context(), app)
		},
	}
	return cmd
}

func DoRebuildImage(ctx context.Context, app *App) error {
	return app.Lifecycle.RebuildImage(ctx)
}
