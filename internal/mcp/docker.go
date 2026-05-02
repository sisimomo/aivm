package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"aivm/internal/run"
)

func FindHostDockerSocket(ctx context.Context, colimaProfile string) (string, error) {
	home, _ := os.UserHomeDir()
	aivmSock := filepath.Join(home, ".colima", colimaProfile, "docker.sock")

	currentCtx, _ := run.Output(ctx, "docker", "context", "show")
	if currentCtx != "" && currentCtx != "colima-"+colimaProfile {
		sockURI, _ := run.Output(ctx, "docker", "context", "inspect", currentCtx, "--format", "{{.Endpoints.docker.Host}}")
		if sockURI != "" {
			rawPath := sockURI
			if len(sockURI) > 7 && sockURI[:7] == "unix://" {
				rawPath = sockURI[7:]
			}
			if rawPath != aivmSock {
				if fi, err := os.Stat(rawPath); err == nil && (fi.Mode()&os.ModeSocket != 0) {
					return sockURI, nil
				}
			}
		}
	}

	if fi, err := os.Stat("/var/run/docker.sock"); err == nil && (fi.Mode()&os.ModeSocket != 0) {
		target, _ := filepath.EvalSymlinks("/var/run/docker.sock")
		if target != aivmSock {
			return "unix:///var/run/docker.sock", nil
		}
	}

	orbSock := filepath.Join(home, ".orbstack", "run", "docker.sock")
	if fi, err := os.Stat(orbSock); err == nil && (fi.Mode()&os.ModeSocket != 0) {
		return "unix://" + orbSock, nil
	}

	defaultSock := filepath.Join(home, ".colima", "default", "docker.sock")
	if fi, err := os.Stat(defaultSock); err == nil && (fi.Mode()&os.ModeSocket != 0) {
		return "unix://" + defaultSock, nil
	}

	colimaDir := filepath.Join(home, ".colima")
	entries, _ := os.ReadDir(colimaDir)
	for _, entry := range entries {
		if entry.Name() == colimaProfile {
			continue
		}
		sockPath := filepath.Join(colimaDir, entry.Name(), "docker.sock")
		if fi, err := os.Stat(sockPath); err == nil && (fi.Mode()&os.ModeSocket != 0) {
			return "unix://" + sockPath, nil
		}
	}

	return "", fmt.Errorf(`no suitable host Docker runtime found.
MCPJungle requires a Docker runtime separate from the aivm Colima VM.
Options:
  • Docker Desktop:  https://www.docker.com/products/docker-desktop/
  • OrbStack:        https://orbstack.dev/
  • Colima default:  colima start`)
}
