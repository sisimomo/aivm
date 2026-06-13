package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func DestroyCmd(getApp func() (*App, error)) *cobra.Command {
	var keepBase bool
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete the VM and clear base image, bootstrap state, and age tracking files",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoDestroy(cmd.Context(), app, keepBase)
		},
	}
	cmd.Flags().BoolVar(&keepBase, "keep-base", false, "preserve base image and host bootstrap/age state for fast recreate")
	return cmd
}

func DoDestroy(ctx context.Context, app *App, keepBase bool) error {
	return app.Lifecycle.Destroy(ctx, keepBase)
}
