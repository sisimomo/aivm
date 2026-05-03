package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/run"
)

type Manager struct {
	Port          int
	DataDir       string
	DockerHost    string
	DevRoot       string
	ImageTag      string
	ServerMode    string
	ContainerName string
}

func (m *Manager) image() string {
	return "ghcr.io/mcpjungle/mcpjungle:" + m.ImageTag
}

func (m *Manager) containerName() string {
	if m.ContainerName != "" {
		return m.ContainerName
	}
	return "mcpjungle-server"
}

func (m *Manager) serverMode() string {
	return m.ServerMode
}

func (m *Manager) dockerEnv() map[string]string {
	if m.DockerHost != "" {
		return map[string]string{"DOCKER_HOST": m.DockerHost}
	}
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	aivmlog.Step("Starting MCPJungle")

	if err := os.MkdirAll(m.DataDir, 0755); err != nil {
		return err
	}

	if m.IsHealthy(ctx) {
		aivmlog.Info("MCPJungle already running on port %d", m.Port)
		return nil
	}

	w := aivmlog.Writer("mcpjungle")
	img := m.image()
	name := m.containerName()

	aivmlog.Info("Pulling MCPJungle image...")
	run.RunEnv(ctx, w, m.dockerEnv(), "docker", "pull", "--quiet", img)

	// Remove any existing stopped container with the same name before starting.
	run.RunEnv(ctx, w, m.dockerEnv(), "docker", "rm", "-f", name)

	aivmlog.Info("Starting MCPJungle container...")
	if err := run.RunEnv(ctx, w, m.dockerEnv(), "docker", "run", "-d",
		"--name", name,
		"-w", "/data",
		"-e", "SERVER_MODE="+m.serverMode(),
		"-e", "OTEL_ENABLED=false",
		"-e", "MCP_SERVER_INIT_REQ_TIMEOUT_SEC=30",
		"-p", fmt.Sprintf("127.0.0.1:%d:8080", m.Port),
		"-v", m.DataDir+":/data",
		"-v", m.DevRoot+":/host:ro",
		"--health-cmd", "wget -qO- http://localhost:8080/health 2>/dev/null || exit 1",
		"--health-interval", "15s",
		"--health-timeout", "5s",
		"--health-retries", "5",
		"--health-start-period", "15s",
		"--restart", "on-failure",
		"--log-driver", "json-file",
		"--log-opt", "max-size=10m",
		"--log-opt", "max-file=3",
		img,
	); err != nil {
		return fmt.Errorf("docker run: %w", err)
	}

	aivmlog.Info("Waiting for MCPJungle to become healthy...")
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if m.IsHealthy(ctx) {
			aivmlog.Success("MCPJungle is healthy on port %d", m.Port)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("MCPJungle failed to become healthy after 40s")
}

func (m *Manager) Stop(ctx context.Context) error {
	w := aivmlog.Writer("mcpjungle")
	name := m.containerName()
	run.RunEnv(ctx, w, m.dockerEnv(), "docker", "stop", name)
	return run.RunEnv(ctx, w, m.dockerEnv(), "docker", "rm", name)
}

func (m *Manager) IsHealthy(_ context.Context) bool {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", m.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

// Logs streams the MCPJungle container logs to stdout until interrupted.
func (m *Manager) Logs() error {
	aivmlog.Info("MCPJungle container logs (Ctrl-C to stop):")
	name := m.ContainerName
	if name == "" {
		name = "mcpjungle-server"
	}
	cmd := exec.Command("docker", "logs", "-f", name)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+m.DockerHost)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
