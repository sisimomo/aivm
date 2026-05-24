package config

import (
	"fmt"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
)

// CompositionError indicates a problem during configuration composition.
type CompositionError struct {
	Stage  string // "load_config", "merge_agents", "merge_plugins", "load_integrations"
	Reason string
	Err    error
}

func (e *CompositionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("composition: %s: %s: %v", e.Stage, e.Reason, e.Err)
	}
	return fmt.Sprintf("composition: %s: %s", e.Stage, e.Reason)
}

func (e *CompositionError) Unwrap() error {
	return e.Err
}

// CompositionEngine orchestrates the full configuration composition flow:
// 1. Load base configuration (YAML + env + build defaults)
// 2. Load and merge agent definitions
// 3. Load and merge plugin definitions
// 4. Build the effective plugin registry
// 5. Load and merge integration definitions
//
// The goal is to consolidate all merging policy and precedence rules in one place,
// making the composition logic explicit and testable.
type CompositionEngine struct {
	Defaults Defaults
}

// CompositionResult is the output of a complete composition run.
type CompositionResult struct {
	// Config is the final, merged configuration.
	Config *Config

	// Plugins is the effective registry of all available plugins (built-in +
	// merged overrides from config and agents).
	Plugins *plugin.Registry

	// Agents is the registry of all available agent providers.
	Agents *agent.Registry

	// ActiveProvider is the default agent provider (from agents.default).
	ActiveProvider agent.Provider

	// ActiveAgentDef is the effective agent definition for the default provider
	// (built-in defaults merged with user overrides).
	ActiveAgentDef agent.Def

	// EnabledAgentDefs is the effective set of agent definitions for ALL enabled
	// agents (those with enable: true in agents.define). Used by bootstrap and
	// persist-dir mounting to set up every enabled agent in the VM.
	EnabledAgentDefs map[string]agent.Def

	// PluginDefs is the effective set of all plugin definitions after merging
	// built-in defaults, agent definitions, and user overrides. Used for config
	// hash computation.
	PluginDefs map[string]plugin.PluginDef

	// Integrations is the complete list of integrations (built-in + config overrides).
	Integrations []integration.IntegrationDef
}

// Compose performs the full configuration composition. It handles all the merging
// logic and returns the final config, plugin registry, and integration definitions.
func (ce *CompositionEngine) Compose(cfgPath string, agents *agent.Registry) (*CompositionResult, error) {
	// Load base configuration.
	cfg, err := Load(cfgPath, ce.Defaults)
	if err != nil {
		return nil, &CompositionError{
			Stage:  "load_config",
			Reason: "failed to load configuration",
			Err:    err,
		}
	}

	enabledAgentNames := cfg.ActiveAgents()
	if len(enabledAgentNames) == 0 {
		return nil, &CompositionError{
			Stage: "load_config",
			Reason: "no agents enabled — add at least one agent to agents.define with enable: true in aivm.yaml\n" +
				"  Example:\n" +
				"    agents:\n" +
				"      default: claude\n" +
				"      define:\n" +
				"        claude:\n" +
				"          enable: true",
		}
	}

	// Validate that all enabled agents are known providers.
	for _, name := range enabledAgentNames {
		if _, ok := agents.Get(name); !ok {
			return nil, &CompositionError{
				Stage:  "load_config",
				Reason: fmt.Sprintf("unknown agent %q in agents.define — check your aivm.yaml", name),
			}
		}
	}

	// Resolve the default agent; auto-infer if only one agent is enabled.
	defaultAgentName := cfg.DefaultAgent()
	if defaultAgentName == "" {
		if len(enabledAgentNames) == 1 {
			defaultAgentName = enabledAgentNames[0]
		} else {
			return nil, &CompositionError{
				Stage:  "load_config",
				Reason: "agents.default must be set when multiple agents are enabled — set it to one of: " + joinNames(enabledAgentNames),
			}
		}
	}

	// Validate that the default agent is in the enabled set.
	defaultEnabled := false
	for _, name := range enabledAgentNames {
		if name == defaultAgentName {
			defaultEnabled = true
			break
		}
	}
	if !defaultEnabled {
		return nil, &CompositionError{
			Stage:  "load_config",
			Reason: fmt.Sprintf("agents.default %q is not enabled — add it to agents.define with enable: true in aivm.yaml", defaultAgentName),
		}
	}

	defaultProv, _ := agents.Get(defaultAgentName)

	// Load built-in agent definitions.
	agentDefs, err := agent.LoadDefs()
	if err != nil {
		return nil, &CompositionError{
			Stage:  "merge_agents",
			Reason: "failed to load built-in agent definitions",
			Err:    err,
		}
	}

	// Merge user agent overrides.
	for name, override := range cfg.Agents.Define {
		base := agentDefs[name]
		agentDefs[name] = agent.MergeDef(base, override)
	}

	enabledAgentDefs := make(map[string]agent.Def, len(enabledAgentNames))
	for _, name := range enabledAgentNames {
		enabledAgentDefs[name] = agentDefs[name]
	}

	activeAgentDef := agentDefs[defaultAgentName]

	// Load built-in plugin definitions.
	pluginDefs, err := plugin.LoadDefaults()
	if err != nil {
		return nil, &CompositionError{
			Stage:  "merge_plugins",
			Reason: "failed to load built-in plugin definitions",
			Err:    err,
		}
	}

	// Convert agent definitions to plugin definitions and merge.
	for name, def := range agentDefs {
		base := pluginDefs[name]
		pluginDefs[name] = plugin.MergePluginDef(base, def.ToPluginDef())
	}

	// Merge user plugin overrides.
	for name, override := range cfg.Plugins.Define {
		base := pluginDefs[name]
		pluginDefs[name] = plugin.MergePluginDef(base, override)
	}

	// Build the plugin registry.
	pluginReg := plugin.NewRegistry()
	for name, def := range pluginDefs {
		pluginReg.Set(plugin.NewYAMLPlugin(name, def))
	}

	// Load integrations.
	integDefs, err := integration.LoadDefaults()
	if err != nil {
		return nil, &CompositionError{
			Stage:  "load_integrations",
			Reason: "failed to load built-in integrations",
			Err:    err,
		}
	}

	// Merge user integration overrides.
	integDefs = append(integDefs, cfg.Integrations...)

	// Auto-inject the t3code plugin when T3 Code mode is enabled, ensuring it's
	// always installed without requiring the user to list it in plugins.enabled.
	if cfg.T3Code.Enable {
		alreadyListed := false
		for _, name := range cfg.Plugins.Enabled {
			if name == "t3code" {
				alreadyListed = true
				break
			}
		}
		if !alreadyListed {
			cfg.Plugins.Enabled = append(cfg.Plugins.Enabled, "t3code")
		}
	}

	return &CompositionResult{
		Config:           cfg,
		Plugins:          pluginReg,
		Agents:           agents,
		ActiveProvider:   defaultProv,
		ActiveAgentDef:   activeAgentDef,
		EnabledAgentDefs: enabledAgentDefs,
		PluginDefs:       pluginDefs,
		Integrations:     integDefs,
	}, nil
}

// joinNames returns a comma-separated string of names for error messages.
func joinNames(names []string) string {
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%q", n)
	}
	return result
}
