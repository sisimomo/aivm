package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
	"aivm/internal/vm"
)

func RebuildImageCmd(app *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rebuild-image",
		Short: "Rebuild the base VM image by re-running bootstrap",
		Long: `Rebuild the base VM image by fully re-running the bootstrap process.

This replaces the existing base image. All future VMs will be based on the
new image. Currently running or suspended sessions will be considered legacy
after this operation and may experience inconsistency.

The VM must be running before invoking this command.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoRebuildImage(cmd.Context(), app, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

func DoRebuildImage(ctx context.Context, app *App, force bool) error {
	status, err := app.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	imgMgr := vm.NewImageManager(app.VM, app.Config.StateDir)
	current := imgMgr.LoadBaseImage()

	fmt.Println()
	aivmlog.Warn("Base image rebuild requested.")
	if current != nil {
		aivmlog.Warn("Current base image: id=%s, created %s",
			current.ID, current.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		aivmlog.Warn("No existing base image found.")
	}

	sessions, _ := app.Sessions.List()
	if len(sessions) > 0 {
		aivmlog.Warn("%d active session(s) will become legacy after this rebuild.", len(sessions))
	}

	aivmlog.Warn("All future VMs will use the new base image.")
	aivmlog.Warn("Existing stopped VMs may continue but are considered legacy.")

	if !force {
		fmt.Printf("\n  Proceed with base image rebuild? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			aivmlog.Info("Rebuild cancelled.")
			return nil
		}
	}

	aivmlog.Step("Rebuilding base image — running full bootstrap")

	if err := DoBootstrap(ctx, app, "", true); err != nil {
		return fmt.Errorf("bootstrap during rebuild: %w", err)
	}

	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("saving base image: %w", err)
	}

	imgMgr.RecordVMImageRef(img.ID)

	aivmlog.Success("Base image rebuilt: %s (id=%s)", img.SnapshotName, img.ID)
	aivmlog.Info("Future VMs will start from this image. Running VMs are now legacy.")
	return nil
}
