package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	aivmlog "aivm/internal/log"
	"aivm/internal/run"
)

type Manager struct {
	ComposeFile string
	Port        int
	DataDir     string
	DockerHost  string
	DevRoot     string
	ImageTag    string
	ServerMode  string
}

func (m *Manager) env() map[string]string {
	return map[string]string{
		"DOCKER_HOST":           m.DockerHost,
		"MCPJUNGLE_PORT":        fmt.Sprintf("%d", m.Port),
		"MCPJUNGLE_DATA_DIR":    m.DataDir,
		"AIVM_DEV_ROOT":         m.DevRoot,
		"MCPJUNGLE_IMAGE_TAG":   m.ImageTag,
		"MCPJUNGLE_SERVER_MODE": m.ServerMode,
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

	if m.ComposeFile == "" {
		return fmt.Errorf("docker-compose.mcpjungle.yml not found — check RepoRoot config")
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

func FindComposeFile(repoRoot string) (string, error) {
	candidates := []string{
		filepath.Join(repoRoot, "docker-compose.mcpjungle.yml"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("docker-compose.mcpjungle.yml not found in %s", repoRoot)
}
