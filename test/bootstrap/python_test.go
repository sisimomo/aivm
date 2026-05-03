//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Python verifies pyenv-based Python installation with the default
// version and validates skip_if idempotency.
func TestPlugin_Python(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("python", nil)
	h.AssertCommand(`
		export PYENV_ROOT="$HOME/.pyenv"
		export PATH="$PYENV_ROOT/bin:$PATH"
		eval "$(pyenv init -)"
		python --version
	`, "Python 3.12.7")
	h.AssertSkipIf("python", nil)
}

// TestPlugin_Python_CustomVersion installs an alternate Python version to confirm
// the pyenv version template substitution works end-to-end.
func TestPlugin_Python_CustomVersion(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	cfg := map[string]any{"version": "3.11.11"}
	h.Install("python", cfg)
	h.AssertCommand(`
		export PYENV_ROOT="$HOME/.pyenv"
		export PATH="$PYENV_ROOT/bin:$PATH"
		eval "$(pyenv init -)"
		python --version
	`, "Python 3.11.11")
	h.AssertSkipIf("python", cfg)
}
