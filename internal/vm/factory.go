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
