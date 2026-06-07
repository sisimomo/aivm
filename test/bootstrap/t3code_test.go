//go:build bootstrap

package bootstraptest

import (
	"fmt"
	"testing"

	"github.com/sisimomo/aivm/test/framework"
)

// TestPlugin_T3Code verifies that the t3code plugin correctly installs the `t3`
// CLI via mise-managed node and makes it reachable from any shell context.
//
// Regression test for: `nohup: failed to run command 't3': No such file or directory`
//
// Root cause: `npm install -g t3` silently fails because node-pty (a t3 dependency)
// is a native module that requires compilation tools (make, g++). Without
// build-essential installed, node-gyp cannot compile node-pty and npm exits 1 — but
// the setup script had no error checking so bootstrap reported success anyway.
func TestPlugin_T3Code(t *testing.T) {
	h := newBootstrapHarness(t)

	// Install the full t3code plugin (pulls in system → mise → mise-node).
	// With mise-node only, `npm install -g t3` auto-reshims into
	// ~/.local/share/mise/shims (on PATH). A separate mise-npm tool bypasses that
	// hook and would require an explicit `mise reshim`.
	h.Install("t3code", nil)

	// Verify `t3` shim is accessible in a login shell — the same shell context
	// that `nohup t3 serve` runs in when launchT3Code executes.
	h.AssertCommand("command -v t3", "")

	// Verify t3 is actually executable.
	h.AssertCommand("t3 --version", "")

	// Verify skip_if idempotency: after a successful install the plugin must
	// detect itself as already set up and skip re-installation.
	h.AssertSkipIf("t3code", nil)

	// Request a free port from the test framework to avoid CI collisions.
	t3Port := framework.FreePort()

	// Simulate the exact invocation used by launchT3Code:
	//   nohup t3 serve --host 127.0.0.1 --port <N> ...
	// Start it in the background, wait briefly, then assert the process found
	// the binary. If `t3` is missing the log contains "No such file or directory".
	// The dynamic port is exported into the shell environment so both the serve
	// command and subsequent checks use the same allocated port.
	h.AssertCommand(fmt.Sprintf(`
set -euo pipefail
T3_PORT=%d
nohup t3 serve --host 127.0.0.1 --port $T3_PORT >/tmp/t3serve_regression.log 2>&1 &
T3PID=$!
sleep 2
# If the process has already exited, check whether it was a "not found" error.
if ! kill -0 "$T3PID" 2>/dev/null; then
    if grep -q "No such file or directory" /tmp/t3serve_regression.log 2>/dev/null; then
        echo "REGRESSION: t3 binary not found"
        cat /tmp/t3serve_regression.log
        exit 1
    fi
fi
# Either still running (good) or exited for another reason (not our regression).
kill "$T3PID" 2>/dev/null || true
exit 0
`, t3Port), "")
}
