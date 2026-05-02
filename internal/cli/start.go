package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
	"aivm/internal/vm"
)

func StartCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start VM and services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoStart(cmd.Context(), app)
		},
	}
}

func DoStart(ctx context.Context, app *App) error {
	cfg := app.Config

	aivmlog.Step("Starting aivm")

	aivmlog.Info("Ensuring MCPJungle is running...")
	if err := app.MCP.Start(ctx); err != nil {
		return fmt.Errorf("starting MCPJungle: %w", err)
	}

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

	status, err := app.VM.Status(ctx)
	if err != nil {
		return err
	}

	if status == vm.StatusStopped && shouldRecreateVM(app) {
		aivmlog.Step("Deleting aged VM profile '%s'", cfg.VM.Profile)
		if err := app.VM.Destroy(ctx); err != nil {
			return err
		}
		status = vm.StatusNotFound
	}

	wasCreated := (status == vm.StatusNotFound)
	needsStart := (status != vm.StatusRunning)

	os.MkdirAll(filepath.Join(cfg.StateDir, ".claude", "projects"), 0755)

	if err := app.VM.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}

	if wasCreated {
		agePath := filepath.Join(cfg.StateDir, "vm-created-at")
		os.WriteFile(agePath, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
	}

	if needsStart {
		if err := app.VM.WaitReady(ctx, 60*time.Second); err != nil {
			return err
		}
		// Resuming or creating a VM cancels any pending Phase 2 deletion.
		app.Sessions.ClearVMStoppedAt()
	}

	imgMgr := vm.NewImageManager(app.VM, cfg.StateDir)

	if wasCreated {
		// Fresh VM: restore from the current base image to skip bootstrap (fast path).
		// If no base image exists yet, fall through to full bootstrap and save one.
		if !imgMgr.TryRestoreBaseImage(ctx) {
			if err := fullBootstrap(ctx, app, app.VM, true); err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
			img, err := imgMgr.SaveBaseImage(ctx)
			if err != nil {
				aivmlog.Warn("could not save base image (non-fatal): %v", err)
			} else {
				imgMgr.RecordVMImageRef(img.ID)
			}
		} else {
			// Base image restored: clear state so syncBootstrap re-checks what is
			// actually in the VM (the base image may predate plugins added since).
			clearBootstrapState(cfg.StateDir)
			recreated, err := syncBootstrap(ctx, app)
			if err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
			if recreated {
				goto ready
			}
		}
	} else {
		recreated, err := syncBootstrap(ctx, app)
		if err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}
		if recreated {
			goto ready
		}
	}

ready:
	if err := app.Monitor.EnsureRunning(); err != nil {
		aivmlog.Warn("could not start idle monitor: %v", err)
	}

	aivmlog.Success("aivm is ready")
	return nil
}

func shouldRecreateVM(app *App) bool {
	cfg := app.Config
	if cfg.VM.MaxAgeDays <= 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(cfg.StateDir, "vm-created-at"))
	if err != nil {
		return false
	}
	epoch, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return false
	}
	age := int(time.Since(time.Unix(epoch, 0)).Hours() / 24)
	if age < cfg.VM.MaxAgeDays {
		return false
	}
	if !interactive(app) {
		aivmlog.Info("VM is %d days old but running non-interactively — keeping", age)
		return false
	}
	aivmlog.Warn("VM '%s' is %d day(s) old (threshold: %d)", cfg.VM.Profile, age, cfg.VM.MaxAgeDays)
	fmt.Printf("  → Delete and recreate for a clean slate? [y/N] ")
	answer := readAnswer(app)
	return answer == "y" || answer == "Y"
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
