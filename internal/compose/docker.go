package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sisimomo/aivm/internal/run"
)

// FindHostDockerSocket probes for a host-side Docker runtime for compose_file.
func FindHostDockerSocket(ctx context.Context) (string, error) {
	home, _ := os.UserHomeDir()

	currentCtx, _ := run.Output(ctx, "docker", "context", "show")
	if currentCtx != "" {
		sockURI, _ := run.Output(ctx, "docker", "context", "inspect", currentCtx,
			"--format", "{{.Endpoints.docker.Host}}")
		if sockURI != "" {
			rawPath := strings.TrimPrefix(sockURI, "unix://")
			if fi, err := os.Stat(rawPath); err == nil &&
				(fi.Mode()&os.ModeSocket != 0) {
				return sockURI, nil
			}
		}
	}

	if fi, err := os.Stat("/var/run/docker.sock"); err == nil &&
		(fi.Mode()&os.ModeSocket != 0) {
		return "unix:///var/run/docker.sock", nil
	}

	orbSock := filepath.Join(home, ".orbstack", "run", "docker.sock")
	if fi, err := os.Stat(orbSock); err == nil && (fi.Mode()&os.ModeSocket != 0) {
		return "unix://" + orbSock, nil
	}

	return "", HostDockerRuntimeNotFoundError()
}

// HostDockerRuntimeNotFoundError is returned when compose_file is configured but
// no host-side Docker runtime socket can be found.
func HostDockerRuntimeNotFoundError() error {
	return fmt.Errorf(`no suitable host Docker runtime found.
Compose services require a Docker runtime on the host (separate from the aivm VM).
Options:
  • Docker Desktop:  https://www.docker.com/products/docker-desktop/
  • OrbStack:        https://orbstack.dev/`)
}
