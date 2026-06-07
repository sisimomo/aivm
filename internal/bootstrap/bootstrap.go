package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
)

const markerFile = ".aivm-bootstrap-version"

// BootstrapVersion is incremented whenever the bootstrap schema changes.
// The CLI layer embeds this in the host-side bootstrap-state.json so it can
// detect when the schema has changed and trigger a fresh reconcile.
const BootstrapVersion = "2"

// Engine orchestrates VM bootstrap: it runs all configured plugins via Executor
// and then writes the in-VM marker file so future invocations can detect the
// current schema version without reading host-side state.
type Engine struct {
	VM       vm.VM
	Executor *plugin.Executor
	StateDir string
}

// Run executes all configured plugins then writes the in-VM bootstrap marker.
// When force is true every plugin runs unconditionally (no Check); use this on
// a fresh blank VM. When false, already-installed plugins are skipped.
func (e *Engine) Run(ctx context.Context, force bool) error {
	if force {
		slog.Info("Bootstrapping VM")
	} else {
		slog.Info("Reconciling VM bootstrap")
	}

	if err := e.Executor.Run(ctx, force); err != nil {
		return err
	}

	script := fmt.Sprintf(`echo '%s' > ~/%s`, BootstrapVersion, markerFile)
	if err := e.VM.Run(ctx, script, nil); err != nil {
		return fmt.Errorf("writing bootstrap marker: %w", err)
	}

	slog.Info("Bootstrap complete!")
	return nil
}
