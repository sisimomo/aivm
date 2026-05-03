//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Golang verifies gvm-based Go installation with the default version
// and validates skip_if idempotency.
func TestPlugin_Golang(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("golang", nil)
	h.AssertCommand(`
		[[ -s "$HOME/.gvm/scripts/gvm" ]] && source "$HOME/.gvm/scripts/gvm"
		go version
	`, "go1.24.0")
	h.AssertSkipIf("golang", nil)
}

// TestPlugin_Golang_CustomVersion installs an alternate Go version to confirm
// the gvm version template substitution works end-to-end.
func TestPlugin_Golang_CustomVersion(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{"version": "go1.23.0"}
	h.Install("golang", cfg)
	h.AssertCommand(`
		[[ -s "$HOME/.gvm/scripts/gvm" ]] && source "$HOME/.gvm/scripts/gvm"
		go version
	`, "go1.23.0")
	h.AssertSkipIf("golang", cfg)
}
