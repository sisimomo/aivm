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

func StatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show VM and service status",
		RunE: func(cmd *cobra.Command, args []string) error {
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
		remaining := cfg.Idle.Timeout - idle
		if remaining > 0 {
			fmt.Printf("  │  Idle shutdown:     in %s\n", remaining)
		} else {
			fmt.Printf("  │  Idle shutdown:     ⚠️  imminent\n")
		}
	} else {
		fmt.Printf("  │  Idle shutdown:     ─ sessions active\n")
	}

	fmt.Println("  └───────────────────────────────────────────────┘")
	fmt.Println()
	return nil
}
