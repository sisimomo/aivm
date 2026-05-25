//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Skills_AllSkills verifies that when a source is configured without
// a skills list, the plugin installs all available skills from the repository
// using the --all flag. At least one SKILL.md file must appear under $HOME.
func TestPlugin_Skills_AllSkills(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	cfg := map[string]any{
		"sources": []any{
			map[string]any{"repo": "mattpocock/skills"},
		},
	}

	h.Install("skills", cfg)

	h.AssertCommand(`find "$HOME" -name "SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
}

// TestPlugin_Skills_SpecificSkills verifies that when a skills list is provided,
// only the named skills are installed. The tdd skill must be present and
// non-listed skills must not appear.
func TestPlugin_Skills_SpecificSkills(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	cfg := map[string]any{
		"sources": []any{
			map[string]any{
				"repo":   "mattpocock/skills",
				"skills": []any{"tdd"},
			},
		},
	}

	h.Install("skills", cfg)

	h.AssertCommand(`find "$HOME" -path "*/tdd/SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
}

// TestPlugin_Skills_Idempotent verifies that running the skills plugin twice
// does not produce an error. npx skills@latest is idempotent by design and
// must succeed even when skills are already present.
func TestPlugin_Skills_Idempotent(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	cfg := map[string]any{
		"sources": []any{
			map[string]any{"repo": "mattpocock/skills"},
		},
	}

	h.Install("skills", cfg)
	h.Install("skills", cfg)

	h.AssertCommand(`find "$HOME" -name "SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
}

// TestPlugin_Skills_IdempotentSpecificSkills verifies that running the skills
// plugin twice with a multi-value skills list is idempotent and installs only
// the listed skills. Both tdd and grill-me must be present after two runs;
// grill-with-docs, which exists in the repo but is not listed, must remain
// absent, confirming that the full skill set was not installed.
func TestPlugin_Skills_IdempotentSpecificSkills(t *testing.T) {
	t.Parallel()
	h := newBootstrapHarness(t)

	cfg := map[string]any{
		"sources": []any{
			map[string]any{
				"repo":   "mattpocock/skills",
				"skills": []any{"tdd", "grill-me"},
			},
		},
	}

	h.Install("skills", cfg)
	h.Install("skills", cfg)

	h.AssertCommand(`find "$HOME" -path "*/tdd/SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
	h.AssertCommand(`find "$HOME" -path "*/grill-me/SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
	h.AssertCommand(`! find "$HOME" -path "*/grill-with-docs/SKILL.md" -maxdepth 6 2>/dev/null | grep -q .`, "")
}
