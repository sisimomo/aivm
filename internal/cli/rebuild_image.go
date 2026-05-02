package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"aivm/internal/bootstrap"
	"aivm/internal/config"
	aivmlog "aivm/internal/log"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

func RebuildImageCmd(app *App) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rebuild-image",
		Short: "Rebuild the base VM image by re-running bootstrap",
		Long: `Rebuild the base VM image by fully re-running the bootstrap process.

Bootstrap runs on a brand-new blank VM (not restored from a previous image)
so every plugin executes unconditionally from a clean slate.

If active sessions exist you will be asked whether to kill them first (hard
rebuild: destroy & recreate the current VM) or keep them alive (soft rebuild:
bootstrap a temporary second VM, mark the current one as legacy, and let it
auto-delete once all sessions close).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoRebuildImage(cmd.Context(), app, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompts, kill active sessions")
	return cmd
}

func DoRebuildImage(ctx context.Context, app *App, force bool) error {
	cfg := app.Config
	imgMgr := vm.NewImageManager(app.VM, cfg.StateDir)
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
	softTransition := false

	if len(sessions) > 0 {
		aivmlog.Warn("%d active session(s) detected.", len(sessions))

		if force {
			killed := app.Sessions.KillAll()
			aivmlog.Info("Sent SIGTERM to %d session(s).", len(killed))
		} else {
			fmt.Printf("\n  Kill all active sessions now? [y/N] ")
			var ans string
			fmt.Scanln(&ans)
			if ans == "y" || ans == "Y" {
				killed := app.Sessions.KillAll()
				aivmlog.Info("Sent SIGTERM to %d session(s).", len(killed))
			} else {
				fmt.Println()
				aivmlog.Warn("A second VM will be created for the rebuild.")
				aivmlog.Warn("The current VM becomes legacy and will be automatically")
				aivmlog.Warn("deleted once all its sessions close (no timer).")
				fmt.Printf("\n  Proceed with soft rebuild? [y/N] ")
				fmt.Scanln(&ans)
				if ans != "y" && ans != "Y" {
					aivmlog.Info("Rebuild cancelled.")
					return nil
				}
				softTransition = true
			}
		}
	}

	if !softTransition && !force {
		fmt.Printf("\n  Proceed with base image rebuild? [y/N] ")
		var ans string
		fmt.Scanln(&ans)
		if ans != "y" && ans != "Y" {
			aivmlog.Info("Rebuild cancelled.")
			return nil
		}
	}

	if softTransition {
		return doSoftRebuild(ctx, app, imgMgr)
	}
	return doHardRebuild(ctx, app, imgMgr)
}

// doHardRebuild destroys the current VM, starts a fresh one, and runs full bootstrap.
func doHardRebuild(ctx context.Context, app *App, imgMgr *vm.ImageManager) error {
	cfg := app.Config

	aivmlog.Step("Destroying existing VM")
	if err := app.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	if err := rebuildStartVM(ctx, app.VM, cfg); err != nil {
		return err
	}
	if err := rebuildBootstrap(ctx, app.VM, cfg); err != nil {
		return err
	}

	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("saving base image: %w", err)
	}
	imgMgr.RecordVMImageRef(img.ID)
	vm.ClearTransitionState(cfg.StateDir)

	aivmlog.Success("Base image rebuilt: %s (id=%s)", img.SnapshotName, img.ID)
	aivmlog.Info("Future VMs will start from this image.")
	return nil
}

// doSoftRebuild bootstraps a temporary second VM so active sessions on the current VM
// are not disrupted. The current VM is marked legacy and auto-deleted when all its
// sessions close. The new base image snapshot is created on the next fresh VM start.
func doSoftRebuild(ctx context.Context, app *App, imgMgr *vm.ImageManager) error {
	cfg := app.Config
	tempProfile := cfg.VM.Profile + "-rebuild"
	tempVM := vm.NewColima(tempProfile, cfg.StateDir)

	// Clean up any leftover rebuild VM from a previous attempt.
	_ = tempVM.Destroy(ctx)

	aivmlog.Step("Starting temporary rebuild VM '%s'", tempProfile)
	if err := rebuildStartVM(ctx, tempVM, cfg); err != nil {
		return err
	}

	aivmlog.Step("Bootstrapping rebuild VM from scratch")
	if err := rebuildBootstrap(ctx, tempVM, cfg); err != nil {
		_ = tempVM.Destroy(ctx)
		return err
	}

	// Record the new base image version without a snapshot. Colima snapshots are
	// profile-bound and cannot be transferred from the temp profile to the main one.
	// The snapshot will be created the first time a fresh main-profile VM starts.
	if _, err := imgMgr.SaveBaseImageMetadataOnly(); err != nil {
		_ = tempVM.Destroy(ctx)
		return fmt.Errorf("saving base image metadata: %w", err)
	}

	aivmlog.Step("Destroying temporary rebuild VM")
	_ = tempVM.Destroy(ctx)

	// Legacy monitor will destroy the old VM once all its sessions drain.
	ts := &vm.TransitionState{
		LegacyProfile: cfg.VM.Profile,
		NewProfile:    cfg.VM.Profile,
		StartedAt:     time.Now(),
	}
	if err := vm.SaveTransitionState(cfg.StateDir, ts); err != nil {
		aivmlog.Warn("could not save transition state: %v", err)
	}
	if err := app.Monitor.EnsureLegacyMonitorRunning(); err != nil {
		aivmlog.Warn("could not start legacy monitor: %v", err)
	}

	aivmlog.Success("New base image recorded.")
	aivmlog.Info("Legacy VM '%s' will be removed once all sessions close.", cfg.VM.Profile)
	aivmlog.Info("Run 'aivm start' after that to apply the new image.")
	return nil
}

// rebuildStartVM creates and waits for a fresh VM using the given config.
func rebuildStartVM(ctx context.Context, v vm.VM, cfg *config.Config) error {
	opts := vm.StartOptions{
		CPUs:      cfg.VM.CPUs,
		MemoryGiB: cfg.VM.MemoryGiB,
		DiskGiB:   cfg.VM.DiskGiB,
		VMType:    cfg.VM.Type,
		Mounts: []vm.Mount{
			{HostPath: cfg.VM.DevRoot, Writable: true},
			{HostPath: filepath.Join(cfg.StateDir, ".claude", "projects"), Writable: true},
		},
	}
	os.MkdirAll(filepath.Join(cfg.StateDir, ".claude", "projects"), 0755)

	if err := v.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}
	if err := v.WaitReady(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("waiting for VM: %w", err)
	}
	return nil
}

// rebuildBootstrap runs every plugin unconditionally (force=true skips per-plugin Check).
func rebuildBootstrap(ctx context.Context, v vm.VM, cfg *config.Config) error {
	eng := &bootstrap.Engine{
		VM: v,
		Executor: &plugin.Executor{
			Registry:     plugin.Global(),
			Enabled:      cfg.Plugins.Enabled,
			PluginConfig: cfg.Plugins.Config,
			StateDir:     cfg.StateDir,
			VMInst:       v,
		},
		StateDir: cfg.StateDir,
	}
	if err := eng.Run(ctx, true); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return nil
}
