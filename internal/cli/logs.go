package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
)

func LogsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logs [service]",
		Short: "Show logs (mcpjungle | monitor | bootstrap | colima)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := "mcpjungle"
			if len(args) > 0 {
				svc = args[0]
			}
			return DoLogs(cmd.Context(), app, svc)
		},
	}
}

func DoLogs(_ context.Context, app *App, service string) error {
	stateDir := app.Config.StateDir
	switch service {
	case "mcpjungle":
		aivmlog.Info("MCPJungle container logs (Ctrl-C to stop):")
		cmd := exec.Command("docker", "compose", "-f", app.MCP.ComposeFile, "logs", "-f")
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+app.MCP.DockerHost)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "monitor", "idle-monitor":
		return tailFile(filepath.Join(stateDir, "logs", "idle-monitor.log"))
	case "bootstrap":
		return tailFile(filepath.Join(stateDir, "logs", "bootstrap.log"))
	case "colima":
		return tailFile(filepath.Join(stateDir, "logs", "colima.log"))
	default:
		return fmt.Errorf("unknown service: %s\nAvailable: mcpjungle | monitor | bootstrap | colima", service)
	}
}

func tailFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", path)
	}
	cmd := exec.Command("tail", "-f", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
