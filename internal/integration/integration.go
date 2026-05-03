// Package integration provides the cross-cutting configuration layer that
// bridges plugins and agents. An integration runs a configure script when a
// specific plugin is installed AND a specific agent is active.
package integration

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// VMRunner is the subset of vm.VM used by integrations during execution.
type VMRunner interface {
	Run(ctx context.Context, script string, env map[string]string) error
}

// IntegrationDef is the definition of an integration expressed in YAML or Go.
// It is used both for the embedded defaults.yaml and for user-defined integrations
// in aivm.yaml (integrations list).
type IntegrationDef struct {
	// Name is an optional unique identifier. When set it is used as the
	// idempotency key instead of "from:to". Required for agent-only
	// integrations (where From is empty).
	Name string `yaml:"name" mapstructure:"name"`
	// From is the plugin name that must be installed for this integration to
	// run. When empty the integration runs whenever the To agent is active,
	// with no plugin prerequisite.
	From string `yaml:"from" mapstructure:"from"`
	// To is the agent name that must be active for this integration to run.
	To string `yaml:"to" mapstructure:"to"`
	// When describes the condition under which this integration executes.
	// Currently only "installed" is recognised; the field is kept for
	// documentation purposes.
	When string `yaml:"when" mapstructure:"when"`
	// Configure is the shell script executed when all conditions are satisfied.
	Configure string `yaml:"configure" mapstructure:"configure"`
}

// Key returns a stable identifier for this integration used for idempotency
// tracking in BootstrapState. If Name is set it is returned directly;
// otherwise the key is constructed as "from:to".
func (d IntegrationDef) Key() string {
	if d.Name != "" {
		return d.Name
	}
	return d.From + ":" + d.To
}

// LoadDefaults parses the embedded defaults.yaml and returns the built-in
// integration definitions.
func LoadDefaults() ([]IntegrationDef, error) {
	var defs []IntegrationDef
	if err := yaml.Unmarshal(defaultsYAML, &defs); err != nil {
		return nil, err
	}
	return defs, nil
}

// Executor resolves and runs integrations that match the current system state.
type Executor struct {
	// Integrations is the full list of integrations to evaluate.
	Integrations []IntegrationDef
	// InstalledPlugins is the set of plugin names that have been installed.
	InstalledPlugins map[string]bool
	// ActiveAgents is the list of agent names that are currently active.
	ActiveAgents []string
	// AlreadyRan is the set of integration keys that have already been
	// executed and are tracked in bootstrap state.
	AlreadyRan map[string]bool
	// VM is used to run configure scripts inside the VM.
	VM VMRunner
	// Log receives integration output.
	Log io.Writer
	// TemplateVars holds values injected into configure script templates
	// (e.g. mcp_port). Keys match the template placeholders, e.g. {{ .mcp_port }}.
	TemplateVars map[string]any
}

// Matching returns all integrations that should run given the current state,
// excluding those already tracked in AlreadyRan. An integration with an empty
// From field runs whenever the To agent is active (no plugin prerequisite).
func (e *Executor) Matching() []IntegrationDef {
	var out []IntegrationDef
	for _, integ := range e.Integrations {
		// Plugin prerequisite: only checked when From is set.
		if integ.From != "" && !e.InstalledPlugins[integ.From] {
			continue
		}
		if !containsString(e.ActiveAgents, integ.To) {
			continue
		}
		if e.AlreadyRan[integ.Key()] {
			continue
		}
		out = append(out, integ)
	}
	return out
}

// Run executes all matching integrations in order and returns the keys of
// integrations that were successfully executed.
func (e *Executor) Run(ctx context.Context) ([]string, error) {
	matching := e.Matching()
	if len(matching) == 0 {
		return nil, nil
	}

	var ran []string
	for _, integ := range matching {
		if err := ctx.Err(); err != nil {
			return ran, err
		}
		script, err := renderScript(integ.Configure, e.TemplateVars)
		if err != nil {
			return ran, fmt.Errorf("integration %s: render script: %w", integ.Key(), err)
		}
		if e.VM != nil {
			if err := e.VM.Run(ctx, script, nil); err != nil {
				return ran, fmt.Errorf("integration %s: %w", integ.Key(), err)
			}
		}
		ran = append(ran, integ.Key())
	}
	return ran, nil
}

func renderScript(src string, data map[string]any) (string, error) {
	if src == "" {
		return "", nil
	}
	t, err := template.New("").Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
