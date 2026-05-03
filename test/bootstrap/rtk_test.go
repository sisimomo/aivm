//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_RTK verifies rtk (Rust Token Killer) installation and skip_if idempotency.
func TestPlugin_RTK(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("rtk", nil)
	h.AssertCommand("rtk --version", "")
	h.AssertSkipIf("rtk", nil)
}
