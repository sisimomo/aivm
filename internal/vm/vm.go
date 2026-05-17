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
}

type Snapshot struct {
	Name      string
	CreatedAt time.Time
}

type VM interface {
	Profile() string
	// NeedsPortBindingAtBoot reports whether ports must be declared at container
	// creation time (true for Docker) rather than opened via a post-boot tunnel
	// (false for Colima). Callers use this instead of inspecting the backend
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
	SSH(ctx context.Context) error
	WaitReady(ctx context.Context, timeout time.Duration) error
	CreateSnapshot(ctx context.Context, name string) error
	RestoreSnapshot(ctx context.Context, name string) (bool, error)
	ListSnapshots(ctx context.Context) ([]Snapshot, error)
	// TransferSnapshot makes an existing snapshot (created under this VM's
	// profile) accessible under targetProfile. For backends where snapshots
	// are profile-scoped (e.g. Docker image tags), this re-tags the underlying
	// artifact. For backends that cannot transfer snapshots, it returns an
	// error; callers should treat that as non-fatal and clear the snapshot
	// reference so that a full bootstrap is used instead.
	TransferSnapshot(ctx context.Context, snapshotName, targetProfile string) error
}
