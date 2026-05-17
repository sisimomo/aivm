package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func RecreateCmd(getApp func() (*App, error)) *cobra.Command {
	var rebuild bool
	cmd := &cobra.Command{
		Use:   "recreate",
		Short: "Restore VM from base image for a clean environment",
		Long: `Restore the VM from the saved base image snapshot.

By default (fast path), the current VM is stopped, destroyed, and immediately
recreated from the saved base image snapshot — skipping bootstrap entirely.
This is the recommended way to undo changes introduced by an AI agent and
return to a known-good clean state.

If no base image snapshot exists yet, you are prompted to run a full rebuild.

With --rebuild, a full bootstrap is performed instead: the current VM is
destroyed, the bootstrap process runs from scratch on a blank VM, and the
result is saved as the new base image. Use this to refresh the base image
itself when your tooling or plugins have changed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoRecreate(cmd.Context(), app, rebuild)
		},
	}
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "run full bootstrap and save a new base image (slow)")
	return cmd
}

func DoRecreate(ctx context.Context, app *App, rebuild bool) error {
	return app.Lifecycle.RecreateVM(ctx, rebuild)
}
