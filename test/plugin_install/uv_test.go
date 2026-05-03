//go:build plugin_install

package plugininstall

import "testing"

// TestPlugin_UV verifies the standalone uv installer and skip_if idempotency.
func TestPlugin_UV(t *testing.T) {
	t.Parallel()
	h := newPluginHarness(t)
	h.Install("uv", nil)
	h.AssertCommand(`export PATH="$HOME/.local/bin:$PATH" && uv --version`, "uv ")
	h.AssertSkipIf("uv", nil)
}
