//go:build bootstrap

package bootstraptest

import "testing"

// TestIntegration_RTK_Claude installs rtk, then runs integrations for the
// claude agent. This exercises the rtk:claude integration configure script
// (rtk init -g --auto-patch) end-to-end inside a real container.
func TestIntegration_RTK_Claude(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("rtk", nil)
	h.RunIntegrations("claude", nil)
	// Verify rtk is still functional after the global init ran.
	h.AssertCommand("rtk --version", "")
}

// TestIntegration_RTK_Copilot installs rtk, then runs integrations for the
// copilot agent. This exercises the rtk:copilot integration configure script
// (rtk init -g --auto-patch) end-to-end inside a real container.
func TestIntegration_RTK_Copilot(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("rtk", nil)
	h.RunIntegrations("copilot", nil)
	// Verify rtk is still functional after the global init ran.
	h.AssertCommand("rtk --version", "")
}
