package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func RebuildImageCmd(getApp func() (*App, error)) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rebuild-image",
		Short: "Rebuild the base VM image by re-running bootstrap",
		Long: `Rebuild the base VM image by fully re-running the bootstrap process.

Bootstrap runs on a brand-new blank VM (not restored from a previous image)
so every plugin executes unconditionally from a clean slate.

If active sessions exist you will be asked whether to stop them first (hard
rebuild: destroy & recreate the current VM) or keep them alive (soft rebuild:
bootstrap a temporary second VM, mark the current one as legacy, and let it
auto-delete once all sessions close).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoRebuildImage(cmd.Context(), app, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompts, stop active sessions")
	return cmd
}

func DoRebuildImage(ctx context.Context, app *App, force bool) error {
	return app.Lifecycle.RebuildImage(ctx, force)
}
