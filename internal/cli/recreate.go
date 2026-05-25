// Package cli provides the command-line interface for aivm.
package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func RecreateCmd(getApp func() (*App, error)) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "recreate",
		Short: "Recreate the VM from scratch by destroying and re-bootstrapping it",
		Long: `Recreate the VM from scratch by destroying it and running bootstrap on a fresh one.

If active sessions exist you will be asked to confirm stopping them before
the recreation proceeds.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoRecreate(cmd.Context(), app, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompts, stop active sessions")
	return cmd
}

// DoRecreate runs the recreate lifecycle operation for the given app.
func DoRecreate(ctx context.Context, app *App, force bool) error {
	return app.Lifecycle.Recreate(ctx, force)
}
