package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"aivm/internal/vm"
)

func StatusCmd(getApp func() (*App, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show VM and service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := getApp()
			if err != nil {
				return err
			}
			return DoStatus(cmd.Context(), app)
		},
	}
}

func DoStatus(ctx context.Context, app *App) error {
	cfg := app.Config

	fmt.Println()
	fmt.Println("  ┌─ aivm status ─────────────────────────────────┐")

	status, _ := app.VM.Status(ctx)
	vmIcon := "❌"
	if status == vm.StatusRunning {
		vmIcon = "✅"
	}
	fmt.Printf("  │  VM (%s): %s %s\n", cfg.VM.Profile, vmIcon, status)

	imgMgr := vm.NewImageManager(app.VM, cfg.StateDir)
	baseImg := imgMgr.LoadBaseImage()
	if baseImg != nil {
		fmt.Printf("  │  Base image:        id=%s (%s)\n",
			baseImg.ID, baseImg.CreatedAt.Local().Format("2006-01-02 15:04 MST"))
		vmRef := imgMgr.GetVMImageRef()
		if vmRef == "" {
			fmt.Printf("  │  VM image ref:      (unknown)\n")
		} else if imgMgr.IsVMLegacy() {
			fmt.Printf("  │  VM image ref:      id=%s ⚠️  legacy\n", vmRef)
		} else {
			fmt.Printf("  │  VM image ref:      id=%s ✅ current\n", vmRef)
		}
	} else {
		fmt.Printf("  │  Base image:        (none — run 'aivm start' to create)\n")
	}

	// Show legacy VM info when a two-VM transition is in progress.
	if ts := vm.LoadTransitionState(cfg.StateDir); ts != nil {
		legacyVM := vm.NewColima(ts.LegacyProfile, cfg.StateDir)
		legacyStatus, _ := legacyVM.Status(ctx)
		legacyIcon := "❌"
		if legacyStatus == vm.StatusRunning {
			legacyIcon = "🔄"
		}
		legacySessions, _ := app.Sessions.CountLegacy(ts.StartedAt)
		fmt.Printf("  │  ─────────────────────────────────────────────\n")
		fmt.Printf("  │  Legacy VM (%s): %s %s\n", ts.LegacyProfile, legacyIcon, legacyStatus)
		fmt.Printf("  │  Legacy sessions:   %d remaining (auto-delete when done)\n", legacySessions)

		legacyPID := filepath.Join(cfg.StateDir, "legacy-monitor.pid")
		legacyMonIcon := "❌"
		if _, err := os.Stat(legacyPID); err == nil {
			legacyMonIcon = "✅"
		}
		fmt.Printf("  │  Legacy monitor:    %s\n", legacyMonIcon)
		fmt.Printf("  │  Transition since:  %s\n", ts.StartedAt.Local().Format("2006-01-02 15:04 MST"))
		fmt.Printf("  │  ─────────────────────────────────────────────\n")
	}

	mcpIcon := "❌"
	if app.MCP.IsHealthy(ctx) {
		mcpIcon = "✅"
	}
	fmt.Printf("  │  MCPJungle:         %s port %d\n", mcpIcon, cfg.MCP.Port)

	monitorPID := filepath.Join(cfg.StateDir, "idle-monitor.pid")
	monitorIcon := "❌"
	if _, err := os.Stat(monitorPID); err == nil {
		monitorIcon = "✅"
	}
	fmt.Printf("  │  Idle monitor:      %s\n", monitorIcon)

	sessions, _ := app.Sessions.List()
	fmt.Printf("  │  Active sessions:   %d\n", len(sessions))

	if len(sessions) == 0 {
		last := app.Sessions.ReadLastActive()
		idle := time.Since(last).Round(time.Second)

		switch status {
		case vm.StatusRunning:
			remaining := cfg.Idle.Timeout - idle
			if remaining > 0 {
				fmt.Printf("  │  Idle suspend:      in %s\n", remaining)
			} else {
				fmt.Printf("  │  Idle suspend:      ⚠️  imminent\n")
			}
		case vm.StatusStopped:
			stoppedAt := app.Sessions.ReadVMStoppedAt()
			if !stoppedAt.IsZero() {
				elapsed := time.Since(stoppedAt).Round(time.Second)
				remaining := cfg.Idle.DeleteTimeout - elapsed
				if remaining > 0 {
					fmt.Printf("  │  VM deletion:       in %s\n", remaining)
				} else {
					fmt.Printf("  │  VM deletion:       ⚠️  imminent\n")
				}
			}
		}
	} else {
		fmt.Printf("  │  Idle suspend:      ─ sessions active\n")
	}

	fmt.Println("  └───────────────────────────────────────────────┘")
	fmt.Println()
	return nil
}
