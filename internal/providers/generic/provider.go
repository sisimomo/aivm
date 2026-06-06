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
	if env.CLICommand == "" {
		return nil, fmt.Errorf("%s: cli_command is not configured", p.name)
	}

	script := vm.BuildLaunchScript(env.WorkDir, env.CLICommand, env.LaunchArgs)
	err := env.VM.RunInteractive(ctx, script, env.Env)
	return exitResponse(err)
}

func (p *Provider) Run(ctx context.Context, env agent.RunEnv) (*agent.Response, error) {
	cli := env.CLICommand
	if cli == "" {
		return nil, fmt.Errorf("%s: cli_command is not configured", p.name)
	}
	if len(env.Args) == 0 {
		return nil, fmt.Errorf("%s: no agent arguments", p.name)
	}

	script := vm.BuildRunScript(env.WorkDir, cli, env.Args)
	// Agent CLIs (e.g. Cursor) often require a PTY for normal stdout; use the same
	// interactive SSH path as Launch when the host has a terminal. Headless/CI keeps
	// RunStream so output is not conflated with a controlling TTY.
	if vm.IsTTY() {
		err := env.VM.RunInteractive(ctx, script, env.Env)
		return exitResponse(err)
	}
	code, err := env.VM.RunStream(ctx, script, env.Env)
	if err != nil {
		return nil, err
	}
	return &agent.Response{ExitCode: code}, nil
}

func exitResponse(err error) (*agent.Response, error) {
	if err == nil {
		return &agent.Response{ExitCode: 0}, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return &agent.Response{ExitCode: exitErr.ExitCode()}, nil
	}
	return nil, err
}
