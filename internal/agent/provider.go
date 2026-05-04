package agent

import "context"

// LaunchEnv contains the context passed to a provider when launching an agent session.
type LaunchEnv struct {
	// VMProfile is the Colima profile name for the target VM.
	VMProfile string
	// WorkDir is the absolute path inside the VM where the agent should start.
	WorkDir string
	// Config holds provider-specific configuration from aivm.yaml agent.providers.<name>.
	Config map[string]any
}

// Response is the normalized result of an agent session.
type Response struct {
	// ExitCode is the exit code returned by the agent process.
	ExitCode int
}

// Provider is the contract every AI agent provider must satisfy.
type Provider interface {
	// Name returns the unique identifier for this provider (e.g. "claude", "copilot").
	Name() string
	// Description returns a human-readable description of the provider.
	Description() string
	// RequiredPlugins returns the names of bootstrap plugins this provider depends on.
	RequiredPlugins() []string
	// Launch starts an agent session and blocks until it completes.
	Launch(ctx context.Context, env LaunchEnv) (*Response, error)
}
