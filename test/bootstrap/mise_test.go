//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Mise is a parametrized test suite that exercises the dynamic
// mise-* plugin mechanism end-to-end inside a real Docker container.
// Each entry installs a tool via "mise use --global <tool>@latest", verifies
// that the tool binary works, and confirms skip_if idempotency.
func TestPlugin_Mise(t *testing.T) {
	tests := []struct {
		name      string // plugin name (e.g. "mise-go")
		checkCmd  string // command to verify the installation
		wantSubstr string // substring expected in checkCmd output
	}{
		{
			name:       "mise-go",
			checkCmd:   "go version",
			wantSubstr: "go1.",
		},
		{
			name:       "mise-rust",
			checkCmd:   "rustc --version",
			wantSubstr: "rustc ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newBootstrapHarness(t)
			h.Install(tc.name, nil)
			h.AssertCommand(tc.checkCmd, tc.wantSubstr)
			h.AssertSkipIf(tc.name, nil)
		})
	}
}
