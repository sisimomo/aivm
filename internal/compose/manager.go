package compose

import "context"

// ComposeManager manages docker compose services that run alongside the VM.
// The concrete implementation is Manager; test code may substitute a no-op stub.
type ComposeManager interface {
	// Up starts all compose services. It is idempotent: services that are
	// already running are skipped. Called during VM startup.
	Up(ctx context.Context) error

	// Down stops and removes compose services. When removeVolumes is true,
	// named volumes are also removed (used by `aivm destroy`).
	Down(ctx context.Context, removeVolumes bool) error

	// HealthMap returns a map of service name → healthy for all services
	// defined in the compose file. Used by `aivm status`.
	HealthMap(ctx context.Context) map[string]bool

	// Logs streams docker compose logs for all services to stdout until
	// interrupted.
	Logs() error
}
