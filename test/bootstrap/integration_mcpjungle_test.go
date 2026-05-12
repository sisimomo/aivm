//go:build bootstrap

package bootstraptest

import "testing"

// TestIntegration_MCPJungle_Claude installs the system plugin (which provides
// jq), then runs integrations for the claude agent. This exercises the
// mcpjungle:claude integration, which writes an MCP server entry into
// ~/.claude/mcp-config.json using a templated port value.
//
// It also verifies idempotency: running integrations a second time must be a
// no-op because the skip_if guard in integration.Executor.Run now exits 0.
func TestIntegration_MCPJungle_Claude(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("system", nil) // provides jq, required by the configure script
	vars := map[string]any{"mcp_port": "8472"}

	// First run: the integration has not been applied yet, so it must execute.
	ran := h.RunIntegrations("claude", vars)
	if len(ran) != 1 || ran[0] != "mcpjungle:claude" {
		t.Fatalf("first RunIntegrations: want [mcpjungle:claude], got %v", ran)
	}
	h.AssertIntegrationConfigured("mcpjungle:claude", vars)
	h.AssertCommand(
		`jq -r '.mcpServers.mcpjungle.url' "$HOME/.claude/mcp-config.json"`,
		"http://host.lima.internal:8472/mcp",
	)

	// Second run: skip_if now exits 0 (already configured), so nothing should run.
	ran = h.RunIntegrations("claude", vars)
	if len(ran) != 0 {
		t.Fatalf("second RunIntegrations (idempotency): want nothing to run, got %v", ran)
	}
}

// TestIntegration_MCPJungle_Copilot installs the system plugin (which provides
// jq), then runs integrations for the copilot agent. This exercises the
// mcpjungle:copilot integration, which writes an MCP server entry into
// ~/.config/gh-copilot/mcp-config.json using a templated port value.
//
// It also verifies idempotency: running integrations a second time must be a
// no-op because the skip_if guard in integration.Executor.Run now exits 0.
func TestIntegration_MCPJungle_Copilot(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("system", nil) // provides jq, required by the configure script
	vars := map[string]any{"mcp_port": "8472"}

	// First run: the integration has not been applied yet, so it must execute.
	ran := h.RunIntegrations("copilot", vars)
	if len(ran) != 1 || ran[0] != "mcpjungle:copilot" {
		t.Fatalf("first RunIntegrations: want [mcpjungle:copilot], got %v", ran)
	}
	h.AssertIntegrationConfigured("mcpjungle:copilot", vars)
	h.AssertCommand(
		`jq -r '.mcpServers.mcpjungle.url' "$HOME/.config/gh-copilot/mcp-config.json"`,
		"http://host.lima.internal:8472/mcp",
	)

	// Second run: skip_if now exits 0 (already configured), so nothing should run.
	ran = h.RunIntegrations("copilot", vars)
	if len(ran) != 0 {
		t.Fatalf("second RunIntegrations (idempotency): want nothing to run, got %v", ran)
	}
}
