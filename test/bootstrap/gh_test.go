//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_GH verifies GitHub CLI installation and skip_if idempotency.
func TestPlugin_GH(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("gh", nil)
	h.AssertCommand("gh --version", "gh version")
	h.AssertSkipIf("gh", nil)
}
