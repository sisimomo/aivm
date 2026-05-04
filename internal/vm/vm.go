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

type StartOptions struct {
	CPUs        int
	MemoryBytes int64
	DiskBytes   int64
	VMType      string
	Mounts      []Mount
	SSHAgent    bool
}

type Snapshot struct {
	Name      string
	CreatedAt time.Time
}

type VM interface {
	Profile() string
	Status(ctx context.Context) (Status, error)
	Start(ctx context.Context, opts StartOptions) error
	Stop(ctx context.Context) error
	Destroy(ctx context.Context) error
	Run(ctx context.Context, script string, env map[string]string) error
	// RunOutput executes a script inside the VM and returns its combined
	// stdout+stderr as a string. Use this when you need to act on script output.
	RunOutput(ctx context.Context, script string, env map[string]string) (string, error)
	SSH(ctx context.Context) error
	WaitReady(ctx context.Context, timeout time.Duration) error
	CreateSnapshot(ctx context.Context, name string) error
	RestoreSnapshot(ctx context.Context, name string) (bool, error)
	ListSnapshots(ctx context.Context) ([]Snapshot, error)
}
