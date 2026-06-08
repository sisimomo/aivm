package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/sisimomo/aivm/internal/run"
)

// MisePlugin is a dynamically-synthesised Plugin for any tool managed by mise.
// It is constructed on demand by the registry for any name matching "mise-<tool>",
// where <tool> is the exact tool name recognised by mise (e.g. "node", "go", "rust").
// Users configure versions via plugins.config.<name> in aivm.yaml:
//
//	plugins:
//	  config:
//	    mise-node:
//	      version: "22"               # global version (default: "latest")
//	      extra_versions: ["20", "18"] # also installed, not set as global
type MisePlugin struct {
	name string
	Tool string
}

// NewMisePlugin returns a Plugin for name if name has the "mise-" prefix and a
// non-empty tool component; otherwise returns (nil, false).
func NewMisePlugin(name string) (Plugin, bool) {
	tool, ok := strings.CutPrefix(name, "mise-")
	if !ok || tool == "" {
		return nil, false
	}
	return &MisePlugin{name: name, Tool: tool}, true
}

func (p *MisePlugin) Name() string           { return p.name }
func (p *MisePlugin) Description() string    { return p.Tool + " via mise" }
func (p *MisePlugin) Dependencies() []string { return []string{"mise"} }
func (p *MisePlugin) Agents() []string       { return nil }
func (p *MisePlugin) PathEntries() []string  { return nil }

// Setup installs the global version with `mise use --global`, then installs
// each extra version with `mise install` (without changing the global).
func (p *MisePlugin) Setup(ctx context.Context, env InstallEnv) error {
	version := env.ConfigString("version", "latest")
	extras := env.ConfigStringSlice("extra_versions")

	var sb strings.Builder
	fmt.Fprintf(&sb, "mise use --global %s@%s", p.Tool, version)
	for _, v := range extras {
		fmt.Fprintf(&sb, "\nmise install %s@%s", p.Tool, v)
	}
	script := sb.String()
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil)
	}
	return run.Run(ctx, env.Log, "bash", "-c", script)
}
