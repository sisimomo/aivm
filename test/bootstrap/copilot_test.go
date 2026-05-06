//go:build bootstrap

package bootstraptest

import "testing"

// TestAgent_Copilot verifies the GitHub Copilot CLI extension install and
// skip_if detection. Authentication is not tested here.
func TestAgent_Copilot(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("copilot", nil) // installs system + gh first (dependencies)
	h.AssertCommand("copilot --version 2>&1", "Copilot")
	h.AssertSkipIf("copilot", nil)
}
