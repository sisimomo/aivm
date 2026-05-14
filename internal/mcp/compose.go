package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/template"

	"github.com/sisimomo/aivm/internal/config"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/run"
)

// Manager is the concrete implementation of SidecarManager. It starts and
// stops Docker container sidecars alongside the VM lifecycle.
type Manager struct {
	Sidecars   []config.SidecarConfig
	DockerHost string
	Profile    string
	DataDir    string // aivm state directory, exposed as {{ .aivm_data_dir }}
	HomeDir    string // user's home directory, exposed as {{ .home_dir }}
}

// containerName returns the Docker container name for a sidecar:
// "aivm-<profile>-<sidecar-name>".
func (m *Manager) containerName(sc config.SidecarConfig) string {
	return "aivm-" + m.Profile + "-" + sc.Name
}

// dockerEnv returns the env map with DOCKER_HOST set when configured.
func (m *Manager) dockerEnv() map[string]string {
	if m.DockerHost != "" {
		return map[string]string{"DOCKER_HOST": m.DockerHost}
	}
	return nil
}

// dockerCmd builds an exec.Cmd for a docker command with DOCKER_HOST injected.
func (m *Manager) dockerCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", args...)
	if m.DockerHost != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+m.DockerHost)
	}
	return cmd
}

// interpolateArgs expands Go template variables in sc.Args:
//
//	{{ .aivm_data_dir }} → m.DataDir
//	{{ .profile }}       → m.Profile
//	{{ .home_dir }}      → m.HomeDir
func (m *Manager) interpolateArgs(sc config.SidecarConfig) (string, error) {
	tmpl, err := template.New("args").Parse(sc.Args)
	if err != nil {
		return "", fmt.Errorf("parsing args template for sidecar %q: %w", sc.Name, err)
	}
	vars := map[string]any{
		"aivm_data_dir": m.DataDir,
		"profile":       m.Profile,
		"home_dir":      m.HomeDir,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("executing args template for sidecar %q: %w", sc.Name, err)
	}
	return buf.String(), nil
}

// isContainerRunning returns true if the named container exists and is running.
func (m *Manager) isContainerRunning(ctx context.Context, name string) bool {
	out, err := m.dockerCmd(ctx, "inspect", "--format", "{{.State.Running}}", name).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// isHealthy returns true if the named container is healthy (or running when no
// HEALTHCHECK is defined).
func (m *Manager) isHealthy(ctx context.Context, name string) bool {
	out, err := m.dockerCmd(ctx, "inspect", "--format", "{{.State.Health.Status}}", name).Output()
	if err != nil {
		return false
	}
	status := strings.TrimSpace(string(out))
	// No HEALTHCHECK defined on the image — fall back to the running state.
	if status == "" || status == "<no value>" {
		return m.isContainerRunning(ctx, name)
	}
	return status == "healthy"
}

// startSidecar starts a single sidecar container. It is idempotent: if a
// container with the same name is already running it is skipped.
func (m *Manager) startSidecar(ctx context.Context, sc config.SidecarConfig) error {
	name := m.containerName(sc)
	aivmlog.Step("Starting sidecar %q (%s)", sc.Name, name)

	if m.isContainerRunning(ctx, name) {
		aivmlog.Info("Sidecar %q already running", sc.Name)
		return nil
	}

	interpolated, err := m.interpolateArgs(sc)
	if err != nil {
		return err
	}

	w := aivmlog.Writer("sidecar:" + sc.Name)
	// Remove any stopped or failed container with the same name before starting.
	_ = run.RunEnv(ctx, w, m.dockerEnv(), "docker", "rm", "-f", name)

	aivmlog.Info("Starting sidecar %q...", sc.Name)
	// Execute the interpolated args via shell so they may contain shell
	// constructs (quotes, variables, etc.), but pass the container name
	// safely as a separate docker argument to prevent injection.
	shellCmd := fmt.Sprintf("docker run -d --name \"$CONTAINER_NAME\" %s", interpolated)
	cmd := exec.CommandContext(ctx, "sh", "-c", shellCmd) //nolint:gosec
	if m.DockerHost != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+m.DockerHost, "CONTAINER_NAME="+name)
	} else {
		cmd.Env = append(os.Environ(), "CONTAINER_NAME="+name)
	}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("starting sidecar %q: %w", sc.Name, err)
	}

	aivmlog.Success("Sidecar %q started", sc.Name)
	return nil
}

// stopSidecar stops and removes a single sidecar container.
func (m *Manager) stopSidecar(ctx context.Context, sc config.SidecarConfig) error {
	name := m.containerName(sc)
	w := aivmlog.Writer("sidecar:" + sc.Name)
	_ = run.RunEnv(ctx, w, m.dockerEnv(), "docker", "stop", name)
	return run.RunEnv(ctx, w, m.dockerEnv(), "docker", "rm", name)
}

// stopContainer stops and removes an arbitrary container by name (best-effort).
func (m *Manager) stopContainer(ctx context.Context, name string) {
	w := aivmlog.Writer("sidecar:prune")
	_ = run.RunEnv(ctx, w, m.dockerEnv(), "docker", "stop", name)
	_ = run.RunEnv(ctx, w, m.dockerEnv(), "docker", "rm", name)
}

// orphanedContainers returns the names of containers that match the
// "aivm-<profile>-" prefix but are NOT in the set of known config names.
// It uses `docker ps -a` so stopped containers from previous runs are included.
func (m *Manager) orphanedContainers(ctx context.Context, knownNames map[string]struct{}) []string {
	prefix := "aivm-" + m.Profile + "-"
	// docker --filter name= is a substring match, so we verify the prefix in Go.
	out, err := m.dockerCmd(ctx, "ps", "-a",
		"--filter", "name="+prefix,
		"--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	var orphans []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimPrefix(strings.TrimSpace(line), "/") // modern Docker omits "/" but trim to be safe
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue // substring filter returned a false positive
		}
		sidecarName := strings.TrimPrefix(name, prefix)
		if _, known := knownNames[sidecarName]; !known {
			orphans = append(orphans, name)
		}
	}
	return orphans
}

// pruneUnwanted stops and removes:
//  1. Containers for disabled sidecars (present in config with enabled:false).
//  2. Orphaned containers (matching "aivm-<profile>-*" but not in config at all).
func (m *Manager) pruneUnwanted(ctx context.Context) {
	// Build the set of ALL config names (enabled + disabled).
	allNames := make(map[string]struct{}, len(m.Sidecars))
	for _, sc := range m.Sidecars {
		allNames[sc.Name] = struct{}{}
	}

	// 1. Stop and remove disabled sidecars (both running and non-running containers).
	for _, sc := range m.Sidecars {
		if sc.IsEnabled() {
			continue
		}
		name := m.containerName(sc)
		// Check if container exists in any state (running, exited, created, etc.)
		out, err := m.dockerCmd(ctx, "inspect", "--format", "{{.State.Status}}", name).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			// Container exists in some state, remove it.
			aivmlog.Info("Removing disabled sidecar %q (%s)", sc.Name, name)
			m.stopContainer(ctx, name)
		}
	}

	// 2. Stop orphaned containers (aivm-<profile>-* not in config).
	for _, name := range m.orphanedContainers(ctx, allNames) {
		aivmlog.Info("Removing orphaned sidecar container %q", name)
		m.stopContainer(ctx, name)
	}
}

// StartAll implements SidecarManager. It prunes unwanted sidecars, then starts
// all enabled sidecars in config order.
func (m *Manager) StartAll(ctx context.Context) error {
	m.pruneUnwanted(ctx)
	for _, sc := range m.Sidecars {
		if !sc.IsEnabled() {
			continue
		}
		if err := m.startSidecar(ctx, sc); err != nil {
			return err
		}
	}
	return nil
}

// StopAll implements SidecarManager. It stops and removes all enabled sidecars,
// then prunes any remaining unwanted containers.
// All sidecars are attempted regardless of individual errors; the first error is returned.
func (m *Manager) StopAll(ctx context.Context) error {
	var firstErr error
	for _, sc := range m.Sidecars {
		if !sc.IsEnabled() {
			continue
		}
		if err := m.stopSidecar(ctx, sc); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.pruneUnwanted(ctx)
	return firstErr
}

// HealthMap implements SidecarManager. It returns a map of sidecar name → healthy
// for all enabled sidecars, sorted by name for deterministic display.
func (m *Manager) HealthMap(ctx context.Context) map[string]bool {
	result := make(map[string]bool)
	for _, sc := range m.Sidecars {
		if !sc.IsEnabled() {
			continue
		}
		result[sc.Name] = m.isHealthy(ctx, m.containerName(sc))
	}
	return result
}

// EnabledNames returns the names of all enabled sidecars in config order.
func (m *Manager) EnabledNames() []string {
	var names []string
	for _, sc := range m.Sidecars {
		if sc.IsEnabled() {
			names = append(names, sc.Name)
		}
	}
	return names
}

// SortedHealthMap returns (names, healthMap) with names sorted alphabetically.
// Used by Status display for deterministic output.
func (m *Manager) SortedHealthMap(ctx context.Context) ([]string, map[string]bool) {
	hm := m.HealthMap(ctx)
	names := make([]string, 0, len(hm))
	for n := range hm {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, hm
}

// Logs implements SidecarManager. It streams docker logs for the named sidecar
// to stdout until interrupted.
func (m *Manager) Logs(name string) error {
	var found *config.SidecarConfig
	for i := range m.Sidecars {
		if m.Sidecars[i].Name == name {
			found = &m.Sidecars[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("sidecar %q not found in config", name)
	}

	cName := m.containerName(*found)
	aivmlog.Info("Sidecar %q logs (%s, Ctrl-C to stop):", name, cName)
	cmd := exec.Command("docker", "logs", "-f", cName) //nolint:gosec
	if m.DockerHost != "" {
		cmd.Env = append(os.Environ(), "DOCKER_HOST="+m.DockerHost)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
