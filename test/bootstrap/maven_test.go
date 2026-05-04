//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Maven verifies Apache Maven installation using the pinned default
// version and validates that the version-aware skip_if detects it.
func TestPlugin_Maven(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("maven", nil) // installs system → java → maven
	h.AssertCommand("mvn --version", "Apache Maven 3.9.9")
	h.AssertSkipIf("maven", nil)
}

// TestPlugin_Maven_CustomVersion installs an earlier Maven version to confirm
// the version template substitution works end-to-end.
func TestPlugin_Maven_CustomVersion(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{"version": "3.9.6"}
	h.Install("maven", cfg)
	h.AssertCommand("mvn --version", "Apache Maven 3.9.6")
	h.AssertSkipIf("maven", cfg)
}
