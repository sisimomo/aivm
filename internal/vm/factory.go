package vm

import (
	"fmt"

	"github.com/sisimomo/aivm/internal/config"
)

// NewFromConfig constructs the appropriate VM backend from the given VM config.
// The backend field selects the implementation; "colima" (default) creates a
// ColimaVM, "docker" creates a DockerVM.
func NewFromConfig(cfg *config.VMConfig, stateDir string) (VM, error) {
	switch cfg.Backend {
	case "", "colima":
		return NewColima(cfg.Profile(), stateDir), nil
	case "docker":
		return NewDocker(cfg.Profile(), stateDir, cfg.DockerImage), nil
	default:
		return nil, fmt.Errorf("unknown vm backend %q", cfg.Backend)
	}
}

// NewWithProfile constructs a VM with the same backend and docker image as cfg
// but using the given profile name and stateDir. Use this to create a shadow VM
// (e.g. for rebuild-image) that does not conflict with the running primary VM.
func NewWithProfile(cfg *config.VMConfig, profile, stateDir string) (VM, error) {
	switch cfg.Backend {
	case "", "colima":
		return NewColima(profile, stateDir), nil
	case "docker":
		return NewDocker(profile, stateDir, cfg.DockerImage), nil
	default:
		return nil, fmt.Errorf("unknown vm backend %q", cfg.Backend)
	}
}
