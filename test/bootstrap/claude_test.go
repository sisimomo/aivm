//go:build bootstrap

package bootstraptest

import "testing"

// TestAgent_Claude verifies the Claude Code CLI install script works.
// Authentication is not tested here.
func TestAgent_Claude(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("claude", nil) // installs nodejs first (dependency)
	h.AssertCommand(`
		export PATH="$HOME/.claude/local/bin:$HOME/.local/bin:$PATH"
		claude --version
	`, "")
}
