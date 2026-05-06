package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
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
	// Log receives all user-visible output. When nil, aivmlog.Default is used.
	// Inject a custom logger in tests to capture output.
	Log *aivmlog.Logger
}

func (e *Executor) log() *aivmlog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return aivmlog.Default
}

// Ordered returns the enabled plugins in topological order.
func (e *Executor) Ordered() ([]Plugin, error) {
	return e.Registry.Resolve(e.Enabled)
}

// Run executes all enabled plugins in DAG order.
//
// When force is true every plugin's Install+Configure steps run unconditionally
// (Check is skipped). Use force=true on a fresh blank VM; force=false on an
// existing VM so already-installed plugins are skipped.
//
// Dependencies that are not in the Enabled list but are required by an enabled
// plugin are installed automatically. A log message is emitted to make this
// transparent to the user.
func (e *Executor) Run(ctx context.Context, force bool) error {
	ordered, err := e.Ordered()
	if err != nil {
		return fmt.Errorf("resolving plugin order: %w", err)
	}

	if err := e.writePathFile(ctx, ordered); err != nil {
		return fmt.Errorf("writing path file: %w", err)
	}

	// Build a set of explicitly enabled plugins so we can identify auto-pulled deps.
	explicitlyEnabled := make(map[string]bool, len(e.Enabled))
	for _, name := range e.Enabled {
		explicitlyEnabled[name] = true
	}

	// Build a reverse-dependency map so we can report which plugin(s) required a dep.
	// dependents[dep] = list of explicitly-enabled plugins that (transitively) need it.
	dependents := make(map[string][]string)
	var collectDeps func(root, name string)
	collectDeps = func(root, name string) {
		p, ok := e.Registry.Get(name)
		if !ok {
			return
		}
		for _, dep := range p.Dependencies() {
			if !explicitlyEnabled[dep] {
				// dep is implicit — record root as the reason.
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

		// If this plugin was not explicitly requested, log why it is being installed.
		if !explicitlyEnabled[p.Name()] {
			if roots := dependents[p.Name()]; len(roots) > 0 {
				e.log().Info("auto-installing %s (required by: %s)", p.Name(), joinNames(roots))
			}
		}

		cfg := e.PluginConfig[p.Name()]
		if cfg == nil {
			cfg = map[string]any{}
		}
		env := InstallEnv{
			Config:   cfg,
			StateDir: e.StateDir,
			Log:      e.log().Writer(p.Name()),
			VM:       e.VMInst,
		}

		if !force {
			skip, err := p.SkipIf(ctx, env)
			if err != nil {
				e.log().Warn("skip_if failed for plugin %s: %v", p.Name(), err)
			}
			if skip {
				e.log().Info("skip %s (already set up)", p.Name())
				continue
			}
		}

		e.log().Step("Plugin: %s", p.Name())
		start := time.Now()

		if err := p.Setup(ctx, env); err != nil {
			return fmt.Errorf("setup %s: %w", p.Name(), err)
		}

		e.log().Success("%s set up (%s)", p.Name(), time.Since(start).Round(time.Second))
	}
	return nil
}

// joinNames joins a slice of plugin names into a human-readable string.
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

// writePathFile collects path_entries from all enabled plugins (in DAG order,
// deduped) and writes /etc/profile.d/aivm-path.sh so every login shell
// picks them up. It is called once at the start of Run, before any plugin
// setup executes, so the PATH is ready for any subsequent setup scripts that
// rely on it.
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
	return run.Run(ctx, e.log().Writer("path"), "bash", "-c", script)
}
