package agent

import "github.com/sisimomo/aivm/internal/plugin"

// Def is the definition of an AI agent: how to set it up in the VM
// and its runtime launch settings.
// This is semantically distinct from plugin.PluginDef — agents are not plugins.
type Def struct {
	Description  string `yaml:"description"  mapstructure:"description"`
	Dependencies []string `yaml:"dependencies" mapstructure:"dependencies"`
	SkipIf       string `yaml:"skip_if"      mapstructure:"skip_if"`
	Setup        string `yaml:"setup"        mapstructure:"setup"`
	LaunchCommand string `yaml:"launch_command" mapstructure:"launch_command"`
}

// ToPluginDef converts this agent definition into a plugin.PluginDef so it
// can be registered in the plugin bootstrap registry for VM provisioning.
func (d Def) ToPluginDef() plugin.PluginDef {
	return plugin.PluginDef{
		Description:  d.Description,
		Dependencies: d.Dependencies,
		SkipIf:       d.SkipIf,
		Setup:        d.Setup,
	}
}

// MergeDef merges override into base field-by-field; non-zero override fields win.
func MergeDef(base, override Def) Def {
	result := base
	if override.Description != "" {
		result.Description = override.Description
	}
	if len(override.Dependencies) > 0 {
		result.Dependencies = override.Dependencies
	}
	if override.SkipIf != "" {
		result.SkipIf = override.SkipIf
	}
	if override.Setup != "" {
		result.Setup = override.Setup
	}
	if override.LaunchCommand != "" {
		result.LaunchCommand = override.LaunchCommand
	}
	return result
}
