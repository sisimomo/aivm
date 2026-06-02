package agent

import "context"

// VMRuntime is the subset of the vm.VM interface that providers require for
// interactive agent execution. Defined here so the agent package stays
// independent of any concrete VM backend.
type VMRuntime interface {
	Profile() string
	RunInteractive(ctx context.Context, script string, env map[string]string) error
}

// LaunchEnv contains the context passed to a provider when launching an agent session.
type LaunchEnv struct {
	// VM is the runtime that will execute the agent's interactive session.
	VM VMRuntime
	// WorkDir is the absolute path inside the VM where the agent should start.
	WorkDir string
	// Config holds provider-specific configuration from aivm.yaml agent.providers.<name>.
	Config map[string]any
	// Env holds resolved vm.session_env variables for this launch.
	Env map[string]string
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
