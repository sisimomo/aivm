package config

import (
	"fmt"
	"strings"

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

	// ActiveProvider is the selected active agent provider.
	ActiveProvider agent.Provider

	// ActiveAgentDef is the effective agent definition for the active provider
	// (built-in defaults merged with user overrides).
	ActiveAgentDef agent.Def

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

	// Validate that an agent is configured.
	activeAgents := cfg.ActiveAgents()
	if len(activeAgents) == 0 {
		return nil, &CompositionError{
			Stage:  "load_config",
			Reason: "no agent configured — set agents.enabled in aivm.yaml",
		}
	}

	providerName := cfg.Agents.Enabled
	prov, ok := agents.Get(providerName)
	if !ok {
		return nil, &CompositionError{
			Stage:  "load_config",
			Reason: fmt.Sprintf("unknown agent provider %q — check agents.enabled in aivm.yaml", providerName),
		}
	}

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

	// Get the effective agent definition for the active provider.
	activeAgentDef := agentDefs[providerName]

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

	// When MCP Jungle is disabled, filter out integrations that are MCP Jungle-
	// specific (those whose key starts with "mcpjungle:"). Running them when
	// the service is disabled would silently misconfigure agent tooling.
	if !cfg.MCP.Enable {
		filtered := integDefs[:0]
		for _, d := range integDefs {
			if !strings.HasPrefix(d.Key(), "mcpjungle:") {
				filtered = append(filtered, d)
			}
		}
		integDefs = filtered
	}

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
		Config:         cfg,
		Plugins:        pluginReg,
		Agents:         agents,
		ActiveProvider: prov,
		ActiveAgentDef: activeAgentDef,
		PluginDefs:     pluginDefs,
		Integrations:   integDefs,
	}, nil
}
