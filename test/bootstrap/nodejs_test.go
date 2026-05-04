//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_NodeJS verifies nvm-based Node.js installation with the default
// version (22) and validates skip_if idempotency.
func TestPlugin_NodeJS(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("nodejs", nil)
	// nvm adds node to PATH for login shells; source nvm.sh before checking.
	h.AssertCommand(`
		export NVM_DIR="$HOME/.nvm"
		[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"
		node --version
	`, "v22.")
	h.AssertSkipIf("nodejs", nil)
}

// TestPlugin_NodeJS_CustomVersion installs an alternate Node.js major version
// to confirm the nvm version template substitution works.
func TestPlugin_NodeJS_CustomVersion(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{"version": "20"}
	h.Install("nodejs", cfg)
	h.AssertCommand(`
		export NVM_DIR="$HOME/.nvm"
		[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"
		node --version
	`, "v20.")
	h.AssertSkipIf("nodejs", cfg)
}
