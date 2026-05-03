package plugin

import (
	"bytes"
	"context"
	_ "embed"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/sisimomo/aivm/internal/run"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// PluginDef is the definition of a plugin as expressed in YAML.
// It is used both for the embedded defaults.yaml and for user-defined plugins
// in aivm.yaml (plugins.define).
type PluginDef struct {
	Description  string         `yaml:"description"  mapstructure:"description"`
	Dependencies []string       `yaml:"dependencies" mapstructure:"dependencies"`
	// Agents restricts the plugin to the listed provider names.
	// An empty slice means the plugin applies to all providers.
	Agents       []string       `yaml:"agents"       mapstructure:"agents"`
	Defaults     map[string]any `yaml:"defaults"     mapstructure:"defaults"`
	SkipIf       string         `yaml:"skip_if"      mapstructure:"skip_if"`
	Setup        string         `yaml:"setup"        mapstructure:"setup"`
}

// LoadDefaults parses the embedded defaults.yaml and returns plugin definitions keyed by name.
func LoadDefaults() (map[string]PluginDef, error) {
	var defs map[string]PluginDef
	if err := yaml.Unmarshal(defaultsYAML, &defs); err != nil {
		return nil, err
	}
	return defs, nil
}

// MergePluginDef merges override into base field-by-field; non-zero override fields win.
// Defaults maps are merged key-by-key so individual default values can be overridden.
func MergePluginDef(base, override PluginDef) PluginDef {
	result := base
	if override.Description != "" {
		result.Description = override.Description
	}
	if len(override.Dependencies) > 0 {
		result.Dependencies = override.Dependencies
	}
	if len(override.Agents) > 0 {
		result.Agents = override.Agents
	}
	if override.SkipIf != "" {
		result.SkipIf = override.SkipIf
	}
	if override.Setup != "" {
		result.Setup = override.Setup
	}
	if len(override.Defaults) > 0 {
		merged := make(map[string]any, len(result.Defaults)+len(override.Defaults))
		for k, v := range result.Defaults {
			merged[k] = v
		}
		for k, v := range override.Defaults {
			merged[k] = v
		}
		result.Defaults = merged
	}
	return result
}

// YAMLPlugin is a plugin defined entirely by inline scripts in YAML.
type YAMLPlugin struct {
	name string
	def  PluginDef
}

// NewYAMLPlugin creates a Plugin from a name and a PluginDef.
func NewYAMLPlugin(name string, def PluginDef) *YAMLPlugin {
	return &YAMLPlugin{name: name, def: def}
}

func (p *YAMLPlugin) Name() string           { return p.name }
func (p *YAMLPlugin) Description() string    { return p.def.Description }
func (p *YAMLPlugin) Dependencies() []string { return p.def.Dependencies }
func (p *YAMLPlugin) Agents() []string       { return p.def.Agents }

// effectiveConfig merges the plugin's bundled default config values with
// per-plugin config from the user's aivm.yaml (user values win).
func (p *YAMLPlugin) effectiveConfig(env InstallEnv) map[string]any {
	cfg := make(map[string]any, len(p.def.Defaults)+len(env.Config))
	for k, v := range p.def.Defaults {
		cfg[k] = v
	}
	for k, v := range env.Config {
		cfg[k] = v
	}
	return cfg
}

func (p *YAMLPlugin) render(src string, data map[string]any) (string, error) {
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

func (p *YAMLPlugin) SkipIf(ctx context.Context, env InstallEnv) (bool, error) {
	if p.def.SkipIf == "" {
		return false, nil
	}
	script, err := p.render(p.def.SkipIf, p.effectiveConfig(env))
	if err != nil {
		return false, err
	}
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil) == nil, nil
	}
	_, err = run.Output(ctx, "bash", "-lc", script)
	return err == nil, nil
}

func (p *YAMLPlugin) Setup(ctx context.Context, env InstallEnv) error {
	if p.def.Setup == "" {
		return nil
	}
	script, err := p.render(p.def.Setup, p.effectiveConfig(env))
	if err != nil {
		return err
	}
	if env.VM != nil {
		return env.VM.Run(ctx, script, nil)
	}
	return run.Run(ctx, env.Log, "bash", "-c", script)
}
