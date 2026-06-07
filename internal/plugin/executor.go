package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/run"
)

// Executor runs plugins in DAG order.
type Executor struct {
	Registry     *Registry
	Enabled      []string
	PluginConfig map[string]map[string]any
	StateDir     string
	// VMInst is passed as InstallEnv.VM so plugins can run commands in the VM.
	VMInst VMRunner
}

// Ordered returns the enabled plugins in topological order.
func (e *Executor) Ordered() ([]Plugin, error) {
	return e.Registry.Resolve(e.Enabled)
}

const maxSetupRetries = 3

func setupRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	return time.Duration(attempt-1) * 10 * time.Second
}

// Run executes all enabled plugins in DAG order.
func (e *Executor) Run(ctx context.Context, force bool) error {
	ordered, err := e.Ordered()
	if err != nil {
		return fmt.Errorf("resolving plugin order: %w", err)
	}

	if err := e.writePathFile(ctx, ordered); err != nil {
		return fmt.Errorf("writing path file: %w", err)
	}

	explicitlyEnabled := make(map[string]bool, len(e.Enabled))
	for _, name := range e.Enabled {
		explicitlyEnabled[name] = true
	}

	dependents := make(map[string][]string)
	var collectDeps func(root, name string)
	collectDeps = func(root, name string) {
		p, ok := e.Registry.Get(name)
		if !ok {
			return
		}
		for _, dep := range p.Dependencies() {
			if !explicitlyEnabled[dep] {
				already := false
				for _, r := range dependents[dep] {
					if r == root {
						already = true
						break
					}
				}
				if !already {
					dependents[dep] = append(dependents[dep], root)
				}
			}
			collectDeps(root, dep)
		}
	}
	for _, name := range e.Enabled {
		collectDeps(name, name)
	}

	for _, p := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.installPlugin(ctx, p, force, explicitlyEnabled, dependents); err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) installPlugin(ctx context.Context, p Plugin, force bool, explicitlyEnabled map[string]bool, dependents map[string][]string) error {
	return aivmlog.WithWriter(p.Name(), func(logW io.Writer) error {
		if !explicitlyEnabled[p.Name()] {
			if roots := dependents[p.Name()]; len(roots) > 0 {
				slog.Info(fmt.Sprintf("auto-installing %s (required by: %s)", p.Name(), joinNames(roots)))
			}
		}

		cfg := e.PluginConfig[p.Name()]
		if cfg == nil {
			cfg = map[string]any{}
		}
		env := InstallEnv{
			Config:   cfg,
			StateDir: e.StateDir,
			Log:      logW,
			VM:       e.VMInst,
		}

		if !force {
			skip, err := p.SkipIf(ctx, env)
			if err != nil {
				slog.Warn(fmt.Sprintf("skip_if failed for plugin %s: %v", p.Name(), err))
			}
			if skip {
				slog.Debug(fmt.Sprintf("skip %s (already set up)", p.Name()))
				return nil
			}
		}

		slog.Info(fmt.Sprintf("Plugin: %s", p.Name()))
		start := time.Now()

		var setupErr error
		for attempt := 1; attempt <= maxSetupRetries; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}

			if attempt > 1 {
				delay := setupRetryDelay(attempt)
				slog.Warn(fmt.Sprintf("setup %s failed (attempt %d/%d): %v — retrying in %s...",
					p.Name(), attempt-1, maxSetupRetries, setupErr, delay))
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return ctx.Err()
				}
				if installed, _ := p.SkipIf(ctx, env); installed {
					setupErr = nil
					break
				}
			}

			setupErr = p.Setup(ctx, env)
			if setupErr == nil {
				break
			}
		}

		if setupErr != nil {
			installed, _ := p.SkipIf(ctx, env)
			if !installed {
				return fmt.Errorf("setup %s: %w", p.Name(), setupErr)
			}
		}

		slog.Info(fmt.Sprintf("%s set up (%s)", p.Name(), time.Since(start).Round(time.Second)))
		return nil
	})
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		out := ""
		for i, n := range names {
			if i > 0 {
				out += ", "
			}
			out += n
		}
		return out
	}
}

func (e *Executor) writePathFile(ctx context.Context, ordered []Plugin) error {
	seen := make(map[string]bool)
	var entries []string
	for _, p := range ordered {
		for _, entry := range p.PathEntries() {
			if !seen[entry] {
				seen[entry] = true
				entries = append(entries, entry)
			}
		}
	}
	if len(entries) == 0 {
		return nil
	}

	content := "# Managed by aivm — do not edit manually\n" +
		"export PATH=\"" + strings.Join(entries, ":") + ":$PATH\"\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	script := fmt.Sprintf(
		"echo '%s' | base64 -d | sudo tee /etc/profile.d/aivm-path.sh > /dev/null\nsudo chmod 0644 /etc/profile.d/aivm-path.sh",
		encoded,
	)

	if e.VMInst != nil {
		return e.VMInst.Run(ctx, script, nil)
	}
	return aivmlog.WithWriter("path", func(w io.Writer) error {
		return run.Run(ctx, w, "bash", "-c", script)
	})
}
