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
// Users configure versions via plugins.config.<name>.version in aivm.yaml.
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

// SkipIf checks whether mise owns the tool installation. Using "mise where"
// instead of a binary PATH check ensures that an externally-installed binary
// does not falsely satisfy the condition.
func (p *misePlugin) SkipIf(ctx context.Context, env InstallEnv) (bool, error) {
	script := fmt.Sprintf("mise where %s >/dev/null 2>&1", p.tool)
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil) == nil, nil
	}
	_, err := run.Output(ctx, "bash", "-lc", script)
	return err == nil, nil
}

func (p *misePlugin) Setup(ctx context.Context, env InstallEnv) error {
	version := env.ConfigString("version", "latest")
	script := fmt.Sprintf("mise use --global %s@%s", p.tool, version)
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil)
	}
	return run.Run(ctx, env.Log, "bash", "-c", script)
}
