//go:build bootstrap

package bootstraptest

import "testing"

// TestMultiAgent_TwoAgentsCoexist verifies that two agents can be installed
// side-by-side in the same VM without interfering with each other.
//
// This exercises the multi-agent bootstrap path: bootstrapEnabledPlugins
// collects plugins from multiple providers and deduplicates shared
// dependencies. opencode and copilot are chosen because they share the
// "system" dependency — confirming that the second agent's install sees
// system's skip_if fire (already installed) rather than re-running it.
//
// After both agents are installed the test confirms:
//   - Both binaries are runnable via a login shell.
//   - Both skip_if scripts detect their respective agents as installed.
//   - The first agent is unaffected by the second agent's installation.
func TestMultiAgent_TwoAgentsCoexist(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	// Install first agent. "system" is installed as a transitive dependency.
	h.Install("opencode", nil)
	h.AssertCommand("opencode --version", "")
	h.AssertSkipIf("opencode", nil)

	// Install second agent. "system" skip_if should fire (already installed),
	// then gh and copilot are added alongside the existing opencode install.
	h.Install("copilot", nil)
	h.AssertCommand("copilot --version 2>&1", "Copilot")
	h.AssertSkipIf("copilot", nil)

	// First agent must still be usable after the second was installed.
	h.AssertCommand("opencode --version", "")
	h.AssertSkipIf("opencode", nil)
}
