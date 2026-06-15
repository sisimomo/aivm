package vmfactory

import (
	"fmt"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/vm"
)

// NewFromConfig constructs the appropriate VM backend from the given VM config.
// The backend field selects the implementation; "lima" (default) creates a
// LimaVM, "docker" creates a DockerVM.
func NewFromConfig(cfg *config.VMConfig, stateDir string) (vm.VM, error) {
	switch cfg.Backend {
	case "", "lima":
		return vm.NewLima(cfg.Profile(), stateDir, cfg.BaseImageEnable), nil
	case "docker":
		return vm.NewDocker(cfg.Profile(), stateDir, cfg.DockerImage, cfg.BaseImageEnable), nil
	default:
		return nil, fmt.Errorf("unknown vm backend %q", cfg.Backend)
	}
}
