package copilot

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/vm"
)

// Provider implements agent.Provider for GitHub Copilot.
type Provider struct{}

// New returns a new GitHub Copilot provider.
func New() *Provider { return &Provider{} }

func (p *Provider) Name() string             { return "copilot" }
func (p *Provider) Description() string      { return "GitHub Copilot" }
func (p *Provider) RequiredPlugins() []string { return []string{"copilot"} }

func (p *Provider) Launch(ctx context.Context, env agent.LaunchEnv) (*agent.Response, error) {
	launchCmd, ok := env.Config["launch_command"].(string)
	if !ok || launchCmd == "" {
		return nil, fmt.Errorf("copilot: launch_command is not configured")
	}

	script := fmt.Sprintf(`
set -e
export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"
if [[ ! -d %s ]]; then
  echo "[aivm] ERROR: VM directory %s does not exist"
  exit 1
fi
cd %s
exec %s
`, vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir), launchCmd)

	err := vm.InteractiveSSH(ctx, env.VMProfile, nil, script)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &agent.Response{ExitCode: exitErr.ExitCode()}, nil
		}
		return nil, err
	}
	return &agent.Response{ExitCode: 0}, nil
}
