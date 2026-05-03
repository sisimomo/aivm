package agent

import "aivm/internal/plugin"

// Def is the definition of an AI agent: how to install it in the VM,
// configure it, and its runtime launch settings.
// This is semantically distinct from plugin.PluginDef — agents are not plugins.
type Def struct {
	Description  string `yaml:"description"  mapstructure:"description"`
	Dependencies []string `yaml:"dependencies" mapstructure:"dependencies"`
	Check        string `yaml:"check"        mapstructure:"check"`
	Install      string `yaml:"install"      mapstructure:"install"`
	Configure    string `yaml:"configure"    mapstructure:"configure"`
	LaunchCommand string `yaml:"launch_command" mapstructure:"launch_command"`
}

// ToPluginDef converts this agent definition into a plugin.PluginDef so it
// can be registered in the plugin bootstrap registry for VM provisioning.
func (d Def) ToPluginDef() plugin.PluginDef {
	return plugin.PluginDef{
		Description:  d.Description,
		Dependencies: d.Dependencies,
		Check:        d.Check,
		Install:      d.Install,
		Configure:    d.Configure,
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
	if override.Check != "" {
		result.Check = override.Check
	}
	if override.Install != "" {
		result.Install = override.Install
	}
	if override.Configure != "" {
		result.Configure = override.Configure
	}
	if override.LaunchCommand != "" {
		result.LaunchCommand = override.LaunchCommand
	}
	return result
}
