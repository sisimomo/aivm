// Package t3code provides the T3 Code web GUI integration.
// T3 Code is an optional interaction mode: when enabled, `aivm launch` starts
// `t3 serve` inside the VM and port-forwards it to the host, allowing users
// to interact with their configured AI agent through a browser instead of a
// terminal session.
package t3code

import "context"

// Manager abstracts the T3 Code tunnel lifecycle. The real implementation
// (Tunnel) manages an SSH port-forward process; tests inject NoopManager to
// avoid real SSH dependencies.
type Manager interface {
	// Launch starts the SSH port-forward tunnel on the host, exposing the
	// t3 serve port running inside the VM. If already running, it is a no-op.
	Launch(ctx context.Context, port int) error

	// Stop kills the SSH port-forward tunnel process. Ignores errors if the
	// tunnel is not running.
	Stop() error

	// IsRunning reports whether the tunnel process is currently active.
	IsRunning() bool
}
