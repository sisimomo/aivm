package claude

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/vm"
)

// Provider implements agent.Provider for Claude Code.
type Provider struct{}

// New returns a new Claude Code provider.
func New() *Provider { return &Provider{} }

func (p *Provider) Name() string             { return "claude" }
func (p *Provider) Description() string      { return "Claude Code (Anthropic)" }
func (p *Provider) RequiredPlugins() []string { return []string{"claude"} }

func (p *Provider) Launch(ctx context.Context, env agent.LaunchEnv) (*agent.Response, error) {
	script := fmt.Sprintf(`
set -e
export PATH="$HOME/.claude/local/bin:$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"
if [[ ! -d %s ]]; then
  echo "[aivm] ERROR: VM directory %s does not exist"
  exit 1
fi
cd %s
exec claude --dangerously-skip-permissions --mcp-config "$HOME/.claude/mcp-config.json"
`, vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir))

	err := vm.InteractiveSSH(ctx, env.VMProfile, nil, script)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &agent.Response{ExitCode: exitErr.ExitCode()}, nil
		}
		return nil, err
	}
	return &agent.Response{ExitCode: 0}, nil
}
