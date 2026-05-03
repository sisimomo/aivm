//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Java verifies Temurin JDK installation with the default version
// and validates that the version-aware skip_if detects the installed JDK.
func TestPlugin_Java(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("java", nil)
	// java -version writes to stderr; redirect so AssertCommand can check it.
	h.AssertCommand("java -version 2>&1", "version")
	h.AssertCommand("javac -version 2>&1", "javac")
	h.AssertSkipIf("java", nil)
}

// TestPlugin_Java_CustomVersion installs a non-default JDK version to confirm
// the version configuration flows through to the apt package name.
func TestPlugin_Java_CustomVersion(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{"version": "21", "distribution": "temurin"}
	h.Install("java", cfg)
	h.AssertCommand("java -version 2>&1", "21")
	h.AssertSkipIf("java", cfg)
}
