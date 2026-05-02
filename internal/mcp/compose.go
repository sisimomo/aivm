package mcp

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	aivmlog "aivm/internal/log"
	"aivm/internal/run"
)

//go:embed docker-compose.mcpjungle.yml
var composeFileContent []byte

type Manager struct {
	ComposeFile   string
	Port          int
	DataDir       string
	DockerHost    string
	DevRoot       string
	ImageTag      string
	ServerMode    string
	ContainerName string
}

func (m *Manager) env() map[string]string {
	name := m.ContainerName
	if name == "" {
		name = "mcpjungle-server"
	}
	return map[string]string{
		"DOCKER_HOST":              m.DockerHost,
		"MCPJUNGLE_PORT":           fmt.Sprintf("%d", m.Port),
		"MCPJUNGLE_DATA_DIR":       m.DataDir,
		"AIVM_DEV_ROOT":            m.DevRoot,
		"MCPJUNGLE_IMAGE_TAG":      m.ImageTag,
		"MCPJUNGLE_SERVER_MODE":    m.ServerMode,
		"MCPJUNGLE_CONTAINER_NAME": name,
	}
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

	aivmlog.Info("Pulling MCPJungle image...")
	run.RunEnv(ctx, w, m.env(), "docker", "compose", "-f", m.ComposeFile, "pull", "--quiet")

	aivmlog.Info("Starting MCPJungle container...")
	if err := run.RunEnv(ctx, w, m.env(), "docker", "compose", "-f", m.ComposeFile, "up", "-d"); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
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
	if m.ComposeFile == "" {
		return nil
	}
	w := aivmlog.Writer("mcpjungle")
	return run.RunEnv(ctx, w, m.env(), "docker", "compose", "-f", m.ComposeFile, "down")
}

func (m *Manager) IsHealthy(_ context.Context) bool {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", m.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

// EnsureComposeFile writes the embedded docker-compose.mcpjungle.yml to stateDir
// and returns its path. Always writes so the file stays in sync with the binary.
func EnsureComposeFile(stateDir string) (string, error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return "", fmt.Errorf("creating state dir: %w", err)
	}
	dest := filepath.Join(stateDir, "docker-compose.mcpjungle.yml")
	if err := os.WriteFile(dest, composeFileContent, 0644); err != nil {
		return "", fmt.Errorf("writing compose file: %w", err)
	}
	return dest, nil
}
