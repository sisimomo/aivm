//go:build bootstrap

package bootstraptest

import "testing"

// TestMultiAgent_TwoAgentsCoexist verifies that two agents can be installed
// side-by-side in the same VM without interfering with each other.
//
// This exercises the multi-agent bootstrap path: bootstrapEnabledPlugins
// collects plugins from multiple providers and deduplicates shared
// dependencies. opencode and copilot share the "system" dependency — both
// agents are installed in one bootstrap via DAG ordering.
//
// After both agents are installed the test confirms:
//   - Both binaries are runnable via a login shell.
//   - The first agent is unaffected by the second agent's installation.
func TestMultiAgent_TwoAgentsCoexist(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	// Install first agent. "system" is installed as a transitive dependency.
	h.Install("opencode", nil)
	h.AssertCommand("opencode --version", "")

	// Install second agent. "system" is already installed; gh and copilot are added.
	h.Install("copilot", nil)
	h.AssertCommand("copilot --version 2>&1", "Copilot")

	// First agent must still be usable after the second was installed.
	h.AssertCommand("opencode --version", "")
}
