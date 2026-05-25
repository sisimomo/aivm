//go:build bootstrap

package bootstraptest

import (
	"slices"
	"testing"
)

// TestPlugin_CocoindexCode verifies that the cocoindex-code plugin correctly
// installs the `ccc` CLI via uv tool install and that skip_if detects the
// installed binary (idempotency). Both template branches are exercised:
//   - "full" (default): installs cocoindex-code[full] with local ML embeddings
//   - "slim": installs the lightweight variant without ML dependencies
func TestPlugin_CocoindexCode(t *testing.T) {
	tests := []struct {
		name    string
		variant string
	}{
		{name: "full", variant: "full"},
		{name: "slim", variant: "slim"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newBootstrapHarness(t)
			cfg := map[string]any{"variant": tc.variant}

			// Install cocoindex-code (pulls in system → mise → mise-uv first).
			h.Install("cocoindex-code", cfg)

			// ccc must be reachable from a login shell. ~/.local/bin is added to
			// PATH by the mise plugin's path_entries via /etc/profile.d/aivm-path.sh.
			h.AssertCommand("command -v ccc", "ccc")

			// The binary must be runnable without error.
			h.AssertCommand("ccc --help 2>&1", "")

			// skip_if must detect the installed binary (idempotency: no re-install).
			h.AssertSkipIf("cocoindex-code", cfg)
		})
	}
}

// TestPlugin_CocoindexCode_SlimWithConfig verifies that when a `config` map is
// provided, the plugin writes ~/.cocoindex_code/global_settings.yml with the
// serialised YAML content (mode 0600) and that skip_if also checks for the
// file presence (idempotency).
func TestPlugin_CocoindexCode_SlimWithConfig(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{
		"variant": "slim",
		"config": map[string]any{
			"embedding": map[string]any{
				"model":    "voyage/voyage-code-3",
				"provider": "litellm",
			},
			"envs": map[string]any{
				"VOYAGE_API_KEY": "test-key-123",
			},
		},
	}

	h.Install("cocoindex-code", cfg)

	// ccc must be installed.
	h.AssertCommand("command -v ccc", "ccc")

	// global_settings.yml must exist with mode 0600.
	h.AssertCommand("test -f \"$HOME/.cocoindex_code/global_settings.yml\"", "")
	h.AssertCommand("stat -c '%a' \"$HOME/.cocoindex_code/global_settings.yml\"", "600")

	// The file must contain the expected YAML structure.
	h.AssertCommand("grep -q '^embedding:' \"$HOME/.cocoindex_code/global_settings.yml\"", "")
	h.AssertCommand("grep -q 'model: voyage/voyage-code-3' \"$HOME/.cocoindex_code/global_settings.yml\"", "")
	h.AssertCommand("grep -q 'provider: litellm' \"$HOME/.cocoindex_code/global_settings.yml\"", "")
	h.AssertCommand("grep -q '^envs:' \"$HOME/.cocoindex_code/global_settings.yml\"", "")
	h.AssertCommand("grep -q 'VOYAGE_API_KEY:' \"$HOME/.cocoindex_code/global_settings.yml\"", "")

	// skip_if must check both ccc binary and config file (idempotency).
	h.AssertSkipIf("cocoindex-code", cfg)
}

// TestIntegration_CocoindexCode_Copilot verifies that when cocoindex-code is
// installed, the copilot MCP integration writes the correct server entry into
// ~/.copilot/mcp-config.json and is idempotent on a second call.
//
// jq is available as a transitive dependency: cocoindex-code → mise-uv → mise
// → system (which installs jq). No copilot agent install is required because
// the configure script only needs jq and basic shell utilities.
func TestIntegration_CocoindexCode_Copilot(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	// Installing cocoindex-code also installs system (→jq), mise, and mise-uv.
	h.Install("cocoindex-code", nil)

	ran := h.RunIntegrations("copilot", nil)
	if !slices.Contains(ran, "cocoindex-code:copilot") {
		t.Fatalf("expected integration %q to run; ran: %v", "cocoindex-code:copilot", ran)
	}

	// skip_if must now exit 0: the entry is present in the config file.
	h.AssertIntegrationConfigured("cocoindex-code:copilot", nil)

	// Verify the exact JSON structure written to the config file.
	h.AssertCommand(`jq -e '.mcpServers."cocoindex-code".type == "local"' "$HOME/.copilot/mcp-config.json"`, "")
	h.AssertCommand(`jq -e '.mcpServers."cocoindex-code".command == "ccc"' "$HOME/.copilot/mcp-config.json"`, "")
	h.AssertCommand(`jq -e '.mcpServers."cocoindex-code".args[0] == "mcp"' "$HOME/.copilot/mcp-config.json"`, "")
	h.AssertCommand(`jq -e '(.mcpServers."cocoindex-code".tools // []) | contains(["*"])' "$HOME/.copilot/mcp-config.json"`, "")

	// Second run must be idempotent: integration must not rewrite the file.
	ran2 := h.RunIntegrations("copilot", nil)
	if slices.Contains(ran2, "cocoindex-code:copilot") {
		t.Fatalf("expected integration %q to be skipped on second run; ran: %v", "cocoindex-code:copilot", ran2)
	}
}

// TestIntegration_CocoindexCode_OpenCode verifies that when cocoindex-code is
// installed, the opencode MCP integration writes the correct server entry into
// ~/.config/opencode/opencode.json and is idempotent on a second call.
func TestIntegration_CocoindexCode_OpenCode(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	h.Install("cocoindex-code", nil)

	ran := h.RunIntegrations("opencode", nil)
	if !slices.Contains(ran, "cocoindex-code:opencode") {
		t.Fatalf("expected integration %q to run; ran: %v", "cocoindex-code:opencode", ran)
	}

	// skip_if must now exit 0.
	h.AssertIntegrationConfigured("cocoindex-code:opencode", nil)

	// Verify the exact JSON structure.
	h.AssertCommand(`jq -e '.mcp."cocoindex-code".type == "local"' "$HOME/.config/opencode/opencode.json"`, "")
	h.AssertCommand(`jq -e '.mcp."cocoindex-code".command[0] == "ccc"' "$HOME/.config/opencode/opencode.json"`, "")
	h.AssertCommand(`jq -e '.mcp."cocoindex-code".command[1] == "mcp"' "$HOME/.config/opencode/opencode.json"`, "")
	h.AssertCommand(`jq -e '.mcp."cocoindex-code".enabled == true' "$HOME/.config/opencode/opencode.json"`, "")

	// Second run must be idempotent.
	ran2 := h.RunIntegrations("opencode", nil)
	if slices.Contains(ran2, "cocoindex-code:opencode") {
		t.Fatalf("expected integration %q to be skipped on second run; ran: %v", "cocoindex-code:opencode", ran2)
	}
}

// TestIntegration_CocoindexCode_Claude verifies that when cocoindex-code is
// installed alongside the Claude Code agent, the claude MCP integration
// registers ccc as an MCP server via `claude mcp add` and that the integration
// is idempotent on a second call.
//
// The claude agent must be installed so that:
//   - The `claude` binary is available at ~/.claude/local/bin/claude (in PATH via
//     /etc/profile.d/aivm-path.sh after Install adds its path_entries).
//   - `claude mcp add` and `claude mcp get` work without authentication (config only).
func TestIntegration_CocoindexCode_Claude(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	h.Install("cocoindex-code", nil)
	h.Install("claude", nil)

	ran := h.RunIntegrations("claude", nil)
	if !slices.Contains(ran, "cocoindex-code:claude") {
		t.Fatalf("expected integration %q to run; ran: %v", "cocoindex-code:claude", ran)
	}

	// AssertIntegrationConfigured runs `claude mcp get cocoindex-code` (the skip_if
	// script) and asserts it exits 0.
	h.AssertIntegrationConfigured("cocoindex-code:claude", nil)

	// Second run must be idempotent: integration must not re-register the server.
	ran2 := h.RunIntegrations("claude", nil)
	if slices.Contains(ran2, "cocoindex-code:claude") {
		t.Fatalf("expected integration %q to be skipped on second run; ran: %v", "cocoindex-code:claude", ran2)
	}
}
