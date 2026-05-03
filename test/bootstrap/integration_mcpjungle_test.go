//go:build bootstrap

package bootstraptest

import "testing"

// TestIntegration_MCPJungle_Claude installs the system plugin (which provides
// jq), then runs integrations for the claude agent. This exercises the
// mcpjungle:claude integration, which writes an MCP server entry into
// ~/.claude/mcp-config.json using a templated port value.
func TestIntegration_MCPJungle_Claude(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("system", nil) // provides jq, required by the configure script
	vars := map[string]any{"mcp_port": "8472"}
	h.RunIntegrations("claude", vars)
	h.AssertIntegrationConfigured("mcpjungle:claude", vars)
	h.AssertCommand(
		`jq -r '.mcpServers.mcpjungle.url' "$HOME/.claude/mcp-config.json"`,
		"http://host.lima.internal:8472/mcp",
	)
}

// TestIntegration_MCPJungle_Copilot installs the system plugin (which provides
// jq), then runs integrations for the copilot agent. This exercises the
// mcpjungle:copilot integration, which writes an MCP server entry into
// ~/.config/gh-copilot/mcp-config.json using a templated port value.
func TestIntegration_MCPJungle_Copilot(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("system", nil) // provides jq, required by the configure script
	vars := map[string]any{"mcp_port": "8472"}
	h.RunIntegrations("copilot", vars)
	h.AssertIntegrationConfigured("mcpjungle:copilot", vars)
	h.AssertCommand(
		`jq -r '.mcpServers.mcpjungle.url' "$HOME/.config/gh-copilot/mcp-config.json"`,
		"http://host.lima.internal:8472/mcp",
	)
}
