//go:build plugin_install

package plugininstall

import "testing"

// TestPlugin_GH verifies GitHub CLI installation and skip_if idempotency.
func TestPlugin_GH(t *testing.T) {
	t.Parallel()
	h := newPluginHarness(t)
	h.Install("gh", nil)
	h.AssertCommand("gh --version", "gh version")
	h.AssertSkipIf("gh", nil)
}
