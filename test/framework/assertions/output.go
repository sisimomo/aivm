package assertions

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	fw "github.com/sisimomo/aivm/test/framework"
)

// OutputContains asserts that the captured stdout contains substr.
func OutputContains(substr string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.Output.Stdout()
		if !strings.Contains(got, substr) {
			return fmt.Errorf("expected stdout to contain %q\ngot:\n%s", substr, got)
		}
		return nil
	}
}

// OutputNotContains asserts that the captured stdout does NOT contain substr.
func OutputNotContains(substr string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.Output.Stdout()
		if strings.Contains(got, substr) {
			return fmt.Errorf("expected stdout NOT to contain %q\ngot:\n%s", substr, got)
		}
		return nil
	}
}

// StderrContains asserts that the captured stderr contains substr.
func StderrContains(substr string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.Output.Stderr()
		if !strings.Contains(got, substr) {
			return fmt.Errorf("expected stderr to contain %q\ngot:\n%s", substr, got)
		}
		return nil
	}
}

// StderrNotContains asserts that the captured stderr does NOT contain substr.
func StderrNotContains(substr string) fw.AssertFunc {
	return func(_ context.Context, h *fw.Harness) error {
		got := h.Output.Stderr()
		if strings.Contains(got, substr) {
			return fmt.Errorf("expected stderr NOT to contain %q\ngot:\n%s", substr, got)
		}
		return nil
	}
}

// OutputMatches asserts that the captured stdout matches the given regular
// expression pattern.
func OutputMatches(pattern string) fw.AssertFunc {
	re := regexp.MustCompile(pattern)
	return func(_ context.Context, h *fw.Harness) error {
		got := h.Output.Stdout()
		if !re.MatchString(got) {
			return fmt.Errorf("expected stdout to match /%s/\ngot:\n%s", pattern, got)
		}
		return nil
	}
}

// VMOutputContains runs script inside the VM and asserts the output contains
// the given substring. This is a convenience wrapper around VMRunOutput.
func VMOutputContains(script, contains string) fw.AssertFunc {
	return VMRunOutput(script, contains)
}
