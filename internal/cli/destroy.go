package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func DestroyCmd(getApp func() (*App, error)) *cobra.Command {
	var keepBase bool
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete the VM (volumes and host state preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoDestroy(cmd.Context(), app, keepBase)
		},
	}
	cmd.Flags().BoolVar(&keepBase, "keep-base", false, "keep base image and host bootstrap state")
	return cmd
}

func DoDestroy(ctx context.Context, app *App, keepBase bool) error {
	return app.Lifecycle.Destroy(ctx, keepBase)
}
