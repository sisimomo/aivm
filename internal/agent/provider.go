package agent

import "context"

// VMRuntime is the subset of the vm.VM interface that providers require for
// interactive agent execution. Defined here so the agent package stays
// independent of any concrete VM backend.
type VMRuntime interface {
	Profile() string
	RunInteractive(ctx context.Context, script string, env map[string]string) error
	// RunStream executes script in the VM without a PTY, streaming stdout/stderr
	// to the host. Returns the remote process exit code.
	RunStream(ctx context.Context, script string, env map[string]string) (int, error)
}

// LaunchEnv contains the context passed to a provider when launching an agent session.
type LaunchEnv struct {
	// VM is the runtime that will execute the agent's interactive session.
	VM VMRuntime
	// WorkDir is the absolute path inside the VM where the agent should start.
	WorkDir    string
	CLICommand string
	LaunchArgs string
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
	// Launch starts an interactive agent session and blocks until it completes.
	Launch(ctx context.Context, env LaunchEnv) (*Response, error)
	// Run executes the agent CLI with user-supplied arguments (non-interactive stream).
	Run(ctx context.Context, env RunEnv) (*Response, error)
}

// RunEnv is the context for aivm agent -- <args>.
type RunEnv struct {
	VM         VMRuntime
	WorkDir    string
	CLICommand string
	Args       []string
	Env        map[string]string
}
