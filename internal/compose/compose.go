package compose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

// Manager is the concrete implementation of ComposeManager. It delegates all
// compose_file lifecycle to docker compose, using a user-provided compose file.
type Manager struct {
	// ComposeFile is the absolute path to the docker compose file. When empty,
	// all operations are no-ops.
	ComposeFile string
	// DockerHost is the DOCKER_HOST value to pass to docker compose commands.
	// When empty, the ambient docker context is used.
	DockerHost string
	// Profile is the compose project name (--project-name). Typically the VM
	// profile name so compose resources are namespaced per aivm profile.
	Profile string
}

// composeCmd builds an exec.Cmd for a docker compose command with --file and
// --project-name set, and DOCKER_HOST injected when configured.
func (m *Manager) composeCmd(ctx context.Context, args ...string) *exec.Cmd {
	baseArgs := []string{"compose", "--file", m.ComposeFile, "--project-name", m.Profile}
	allArgs := append(baseArgs, args...)
	cmd := exec.CommandContext(ctx, "docker", allArgs...)
	if m.DockerHost != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+m.DockerHost)
	}
	return cmd
}

// Up implements ComposeManager. It runs `docker compose up -d`.
// Returns nil without doing anything when ComposeFile is empty.
func (m *Manager) Up(ctx context.Context) error {
	if m.ComposeFile == "" {
		return nil
	}
	aivmlog.Step("Starting compose services")
	w := aivmlog.Writer("compose")
	cmd := m.composeCmd(ctx, "up", "-d")
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}
	aivmlog.Success("Compose services started")
	return nil
}

// Down implements ComposeManager. It runs `docker compose down`.
// Named volumes are always preserved. Returns nil when ComposeFile is empty.
func (m *Manager) Down(ctx context.Context) error {
	if m.ComposeFile == "" {
		return nil
	}
	w := aivmlog.Writer("compose")
	cmd := m.composeCmd(ctx, "down")
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}
	return nil
}

// HealthMap implements ComposeManager. It returns a map of service name →
// running for all services defined in the compose file.
// Returns nil when ComposeFile is empty or on error.
func (m *Manager) HealthMap(ctx context.Context) map[string]bool {
	if m.ComposeFile == "" {
		return nil
	}

	// All services defined in the compose file.
	allOut, err := m.composeCmd(ctx, "config", "--services").Output()
	if err != nil {
		return nil
	}
	allNames := parseLines(string(allOut))
	if len(allNames) == 0 {
		return nil
	}

	// Services currently running.
	runOut, err := m.composeCmd(ctx, "ps", "--services", "--filter", "status=running").Output()
	if err != nil {
		return nil
	}
	running := make(map[string]bool, len(allNames))
	for _, name := range parseLines(string(runOut)) {
		running[name] = true
	}

	result := make(map[string]bool, len(allNames))
	for _, name := range allNames {
		result[name] = running[name]
	}
	return result
}

// Logs implements ComposeManager. It streams `docker compose logs -f` for all
// services to stdout until interrupted.
func (m *Manager) Logs() error {
	if m.ComposeFile == "" {
		return fmt.Errorf("no compose_file configured — add compose_file: to aivm.yaml")
	}
	aivmlog.Info("Compose service logs (Ctrl-C to stop):")
	cmd := m.composeCmd(context.Background(), "logs", "-f") //nolint:contextcheck
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// parseLines splits s on newlines and returns trimmed, non-empty lines.
func parseLines(s string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
