//go:build bootstrap

package bootstraptest

import (
	"testing"
)

// TestPlugin_CocoindexCode verifies that the cocoindex-code plugin correctly
// installs the `ccc` CLI via uv tool install. Both template branches are exercised:
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

			// Install cocoindex-code (pulls in system → mise → mise-uv → mise-node).
			h.Install("cocoindex-code", cfg)

			// ccc must be reachable from a login shell. ~/.local/bin is added to
			// PATH by the mise plugin's path_entries via /etc/profile.d/aivm-path.sh.
			h.AssertCommand("command -v ccc", "ccc")

			// The binary must be runnable without error.
			h.AssertCommand("ccc --help 2>&1", "")
		})
	}
}

// TestPlugin_CocoindexCode_SlimWithConfig verifies that when a `config` map is
// provided, the plugin writes ~/.cocoindex_code/global_settings.yml with the
// serialised YAML content (mode 0600).
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
}

// TestPlugin_CocoindexCode_SkillInstall verifies that the cocoindex-code plugin
// installs the ccc skill via `npx skills@latest add cocoindex-io/cocoindex-code
// --global --all` during setup. At least one SKILL.md must appear under $HOME.
func TestPlugin_CocoindexCode_SkillInstall(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	// Install cocoindex-code (pulls in mise-node which provides npx).
	h.Install("cocoindex-code", nil)

	// skills@latest must have written at least one SKILL.md somewhere under HOME.
	h.AssertCommand(`find "$HOME" -name "SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
}
