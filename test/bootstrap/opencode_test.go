//go:build bootstrap

package bootstraptest

import "testing"

// TestAgent_OpenCode verifies the OpenCode CLI install script works and that
// skip_if detects the installed binary. Authentication is not tested here.
func TestAgent_OpenCode(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("opencode", nil) // installs system first (dependency)
	h.AssertCommand("opencode --version", "")
	h.AssertSkipIf("opencode", nil)
}
