//go:build bootstrap

package bootstraptest

import (
	"testing"
	"time"
)

// TestAgent_OpenCode verifies the OpenCode CLI install script works and that
// skip_if detects the installed binary. Authentication is not tested here.
func TestAgent_OpenCode(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("opencode", nil) // installs system first (dependency)
	h.AssertCommand("opencode --version", "")
	h.AssertSkipIf("opencode", nil)
}

// TestAgent_OpenCode_LaunchStartsTUI verifies that the opencode launch_command
// actually opens the TUI instead of immediately exiting with an error.
//
// Regression test for: launch_command contained --dangerously-skip-permissions
// (a Claude Code flag) which opencode does not recognise. opencode printed its
// help text and exited with code 1, so the TUI never opened when running `aivm`.
func TestAgent_OpenCode_LaunchStartsTUI(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)
	h.Install("opencode", nil)
	// Allow 5 s for the process to prove it stays alive (TUI running).
	// If the launch_command contains an unrecognised flag, opencode exits in <1 s.
	h.AssertLaunchStartsTUI("opencode", 5*time.Second)
}
