package mcp

import "context"

// SidecarManager manages the full set of Docker container sidecars that run
// alongside the VM. The concrete implementation is Manager; test code may
// substitute a no-op stub.
type SidecarManager interface {
	// StartAll starts all enabled sidecars. It is idempotent: sidecars that are
	// already running are skipped. Called during VM startup.
	StartAll(ctx context.Context) error

	// StopAll stops and removes all enabled sidecars. Called on VM stop/destroy
	// and by the idle monitor.
	StopAll(ctx context.Context) error

	// HealthMap returns a map of sidecar name → healthy for all enabled sidecars.
	// Used by `aivm status`.
	HealthMap(ctx context.Context) map[string]bool

	// Logs streams docker logs for the named sidecar to stdout until interrupted.
	Logs(name string) error
}
