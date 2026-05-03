//go:build plugin_install

package plugininstall

import "testing"

// TestAgent_Claude verifies the Claude Code CLI install script works and that
// skip_if detects the installed binary. Authentication is not tested here.
func TestAgent_Claude(t *testing.T) {
	t.Parallel()
	h := newPluginHarness(t)
	h.Install("claude", nil) // installs nodejs first (dependency)
	h.AssertCommand(`
		export PATH="$HOME/.claude/local/bin:$HOME/.local/bin:$PATH"
		claude --version
	`, "")
	h.AssertSkipIf("claude", nil)
}
