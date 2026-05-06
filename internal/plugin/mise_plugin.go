package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/sisimomo/aivm/internal/run"
)

// misePlugin is a dynamically-synthesised Plugin for any tool managed by mise.
// It is constructed on demand by the registry for any name matching "mise-<tool>",
// where <tool> is the exact tool name recognised by mise (e.g. "node", "go", "rust").
// Users configure versions via plugins.config.<name> in aivm.yaml:
//
//	plugins:
//	  config:
//	    mise-node:
//	      version: "22"               # global version (default: "latest")
//	      extra_versions: ["20", "18"] # also installed, not set as global
type misePlugin struct {
	name string
	tool string
}

// newMisePlugin returns a Plugin for name if name has the "mise-" prefix and a
// non-empty tool component; otherwise returns (nil, false).
func newMisePlugin(name string) (Plugin, bool) {
	tool, ok := strings.CutPrefix(name, "mise-")
	if !ok || tool == "" {
		return nil, false
	}
	return &misePlugin{name: name, tool: tool}, true
}

func (p *misePlugin) Name() string           { return p.name }
func (p *misePlugin) Description() string    { return p.tool + " via mise" }
func (p *misePlugin) Dependencies() []string { return []string{"mise"} }
func (p *misePlugin) Agents() []string       { return nil }
func (p *misePlugin) PathEntries() []string  { return nil }

// skipIfScript generates a shell script that returns 0 only when every
// configured version (global + extras) is already installed via mise.
// "latest" is resolved at runtime via `mise latest <tool>`.
func (p *misePlugin) skipIfScript(version string, extras []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb,
		"_check() {\n  local v=\"$1\"\n  if [ \"$v\" = \"latest\" ]; then\n    v=$(mise latest %s 2>/dev/null) || return 1\n  fi\n  mise where %s@\"$v\" >/dev/null 2>&1\n}\n",
		p.tool, p.tool)
	all := make([]string, 0, 1+len(extras))
	all = append(all, version)
	all = append(all, extras...)
	for i, v := range all {
		if i > 0 {
			sb.WriteString(" && ")
		}
		fmt.Fprintf(&sb, "_check %q", v)
	}
	return sb.String()
}

// SkipIf checks that every configured version is installed via mise.
// Returns true (skip setup) only when all required versions are present.
func (p *misePlugin) SkipIf(ctx context.Context, env InstallEnv) (bool, error) {
	version := env.ConfigString("version", "latest")
	extras := env.ConfigStringSlice("extra_versions")
	script := p.skipIfScript(version, extras)
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil) == nil, nil
	}
	_, err := run.Output(ctx, "bash", "-lc", script)
	return err == nil, nil
}

// Setup installs the global version with `mise use --global`, then installs
// each extra version with `mise install` (without changing the global).
func (p *misePlugin) Setup(ctx context.Context, env InstallEnv) error {
	version := env.ConfigString("version", "latest")
	extras := env.ConfigStringSlice("extra_versions")

	var sb strings.Builder
	fmt.Fprintf(&sb, "mise use --global %s@%s", p.tool, version)
	for _, v := range extras {
		fmt.Fprintf(&sb, "\nmise install %s@%s", p.tool, v)
	}
	script := sb.String()
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil)
	}
	return run.Run(ctx, env.Log, "bash", "-c", script)
}
