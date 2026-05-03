//go:build plugin_install

package plugininstall

import "testing"

// TestPlugin_System verifies that the system baseline-package plugin installs
// correctly and that its skip_if script detects the installed state.
func TestPlugin_System(t *testing.T) {
	t.Parallel()
	h := newPluginHarness(t)
	h.Install("system", nil)
	h.AssertCommand("jq --version", "jq-")
	h.AssertCommand("git --version", "git version")
	h.AssertCommand("curl --version", "curl")
	h.AssertSkipIf("system", nil)
}
