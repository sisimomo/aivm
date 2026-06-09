package vm

import (
	"context"
	"time"
)

type Status int

const (
	StatusNotFound Status = iota
	StatusStopped
	StatusRunning
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "Running"
	case StatusStopped:
		return "Stopped"
	default:
		return "NotFound"
	}
}

type Mount struct {
	HostPath string
	Writable bool
}

type PortMapping struct {
	HostPort      int // 0 means "let Docker auto-assign"
	ContainerPort int
}

type StartOptions struct {
	CPUs         int
	MemoryBytes  int64
	DiskBytes    int64
	VMType       string
	Mounts       []Mount
	SSHAgent     bool
	PortMappings []PortMapping // explicit host:container port mappings (used when host port auto-assignment is needed)
	Privileged   bool          // Docker only: run the container in privileged mode
}

type VM interface {
	Profile() string
	// NeedsPortBindingAtBoot reports whether ports must be declared at container
	// creation time (true for Docker) rather than opened via a post-boot tunnel
	// (false for Lima). Callers use this instead of inspecting the backend
	// config string so the decision stays behind the VM seam.
	NeedsPortBindingAtBoot() bool
	Status(ctx context.Context) (Status, error)
	Start(ctx context.Context, opts StartOptions) error
	Stop(ctx context.Context) error
	Destroy(ctx context.Context) error
	Run(ctx context.Context, script string, env map[string]string) error
	// RunOutput executes a script inside the VM and returns its combined
	// stdout+stderr as a string. Use this when you need to act on script output.
	RunOutput(ctx context.Context, script string, env map[string]string) (string, error)
	// RunInteractive executes script inside the VM with a PTY attached, suitable
	// for TUI applications that need an interactive terminal (e.g. agent CLIs).
	RunInteractive(ctx context.Context, script string, env map[string]string) error
	// RunStream executes script without a PTY, streaming stdout/stderr to the
	// host process. Returns the remote exit code.
	RunStream(ctx context.Context, script string, env map[string]string) (int, error)
	// SSH opens an interactive shell. env injects host session variables when
	// non-empty; implementations should preserve their native SSH/exec path when
	// env is nil or empty.
	SSH(ctx context.Context, env map[string]string) error
	// CopyTo copies a file or directory from the host at localPath into the VM
	// at vmPath. When recursive is true, directories are copied recursively.
	CopyTo(ctx context.Context, localPath, vmPath string, recursive bool) error
	// CopyFrom copies a file or directory from the VM at vmPath to localPath on
	// the host. When recursive is true, directories are copied recursively.
	CopyFrom(ctx context.Context, vmPath, localPath string, recursive bool) error
	WaitReady(ctx context.Context, timeout time.Duration) error
	// GetPublishedPort returns the host port that maps to the given container
	// port. For Docker this queries the actual Docker-assigned port (important
	// when the host port was 0 / auto-assigned at container creation). For
	// Lima, which uses an SSH tunnel, host port == container port, so
	// containerPort is returned unchanged.
	GetPublishedPort(containerPort int) (int, error)
}
