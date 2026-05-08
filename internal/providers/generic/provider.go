// Package generic provides a YAML-driven agent provider.
// Adding a new agent requires only a new entry in internal/agent/defaults.yaml —
// no Go code changes are needed.
package generic

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/vm"
)

// Provider implements agent.Provider for any agent described by an agent.Def.
type Provider struct {
	name string
	def  agent.Def
}

// NewFromDef creates a Provider from an agent name and its built-in definition.
func NewFromDef(name string, def agent.Def) *Provider {
	return &Provider{name: name, def: def}
}

func (p *Provider) Name() string        { return p.name }
func (p *Provider) Description() string { return p.def.Description }

// RequiredPlugins returns the plugin whose name matches the agent — the
// bootstrap engine will install it (and its declared dependencies) in the VM.
func (p *Provider) RequiredPlugins() []string { return []string{p.name} }

func (p *Provider) Launch(ctx context.Context, env agent.LaunchEnv) (*agent.Response, error) {
	launchCmd, ok := env.Config["launch_command"].(string)
	if !ok || launchCmd == "" {
		return nil, fmt.Errorf("%s: launch_command is not configured", p.name)
	}

	// The session runs as `bash -l` (login shell), so the PATH configured
	// by the agent's setup script is already available here.
	script := fmt.Sprintf(`
set -e
if [[ ! -d %s ]]; then
  echo "[aivm] ERROR: VM directory %s does not exist"
  exit 1
fi
cd %s
exec %s
`, vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir), vm.ShellEscape(env.WorkDir), launchCmd)

	err := env.VM.RunInteractive(ctx, script, nil)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &agent.Response{ExitCode: exitErr.ExitCode()}, nil
		}
		return nil, err
	}
	return &agent.Response{ExitCode: 0}, nil
}
