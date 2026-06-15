//go:build bootstrap

package bootstraptest

import "testing"

// TestMultiAgent_TwoAgentsCoexist verifies that two agents can be installed
// side-by-side in the same VM without interfering with each other.
//
// This exercises the multi-agent bootstrap path: bootstrapEnabledPlugins
// collects plugins from multiple providers and deduplicates shared
// dependencies. opencode and copilot share the "mise" plugin dependency — both
// agents are installed in one bootstrap via DAG ordering.
//
// After both agents are installed the test confirms:
//   - opencode is installed via mise (Bun binary; cannot exec under docker exec).
//   - copilot is runnable via a login shell.
//   - The first agent is unaffected by the second agent's installation.
func TestMultiAgent_TwoAgentsCoexist(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	opencodeInstalled := `test -x "$(mise which opencode)" && echo opencode`

	// Install first agent. "system" is installed as a transitive dependency.
	h.Install("opencode", nil)
	h.AssertCommand(opencodeInstalled, "opencode")

	// Install second agent. mise is already installed; mise-copilot is added.
	h.Install("copilot", nil)
	h.AssertCommand("copilot --version 2>&1", "Copilot")

	// First agent must still be installed after the second was installed.
	h.AssertCommand(opencodeInstalled, "opencode")
}
