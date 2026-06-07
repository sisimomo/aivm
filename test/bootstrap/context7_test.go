//go:build bootstrap

package bootstraptest

import (
	"testing"
)

// TestPlugin_Context7 verifies that the context7 plugin installs the ctx7 CLI
// via npm and that skip_if detects the installed binary (idempotency).
func TestPlugin_Context7(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	h.Install("context7", nil)

	h.AssertCommand("command -v ctx7", "ctx7")
	h.AssertCommand("ctx7 --help 2>&1", "Usage")

	h.AssertSkipIf("context7", nil)
}

// TestPlugin_Context7_SkillInstall verifies that the context7 plugin installs
// the find-docs skill via `npx skills@latest add upstash/context7 --global
// --yes --agent '*' --skill find-docs` during setup.
func TestPlugin_Context7_SkillInstall(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	h.Install("context7", nil)

	h.AssertCommand(`find "$HOME" -path "*/skills/find-docs/SKILL.md" 2>/dev/null | grep -q .`, "")
}
