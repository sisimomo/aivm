//go:build bootstrap

package bootstraptest

import "testing"

// TestAgent_Cursor verifies the Cursor Agent install script works.
// Authentication is not tested here.
func TestAgent_Cursor(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("cursor", nil) // installs system first (dependency)
	h.AssertCommand("agent --version", "")
	h.AssertCommand("cursor-agent --version", "")
}
