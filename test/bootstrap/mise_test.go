//go:build bootstrap

package bootstraptest

import "testing"

// TestPlugin_Mise is a parametrized test suite that exercises the dynamic
// mise-* plugin mechanism end-to-end inside a real Docker container.
// Each entry installs a tool via "mise use --global <tool>@<version>" and
// verifies that the tool binary works.
func TestPlugin_Mise(t *testing.T) {
	tests := []struct {
		name        string         // subtest label
		plugin      string         // plugin name passed to Install
		config      map[string]any // nil = use defaults (version: latest)
		checkCmd    string         // command to run inside the container
		wantSubstr  string         // expected substring in checkCmd output
		extraChecks []struct {     // additional post-install assertions
			cmd        string
			wantSubstr string
		}
	}{
		{
			// Installs mise itself (the runtime manager) in isolation.
			// Validates that the binary works and that the bashrc activation
			// line is written.
			name:       "mise-standalone",
			plugin:     "mise",
			checkCmd:   "mise --version",
			wantSubstr: "",
			extraChecks: []struct {
				cmd        string
				wantSubstr string
			}{
				{"grep 'mise activate' ~/.bashrc", "mise activate"},
			},
		},
		{
			name:       "mise-go",
			plugin:     "mise-go",
			checkCmd:   "go version",
			wantSubstr: "go1.",
		},
		{
			name:       "mise-rust",
			plugin:     "mise-rust",
			checkCmd:   "rustc --version",
			wantSubstr: "rustc ",
		},
		{
			// Installs node@22 as global and node@20 as an extra version.
			// Verifies the global binary works and that the extra version is present.
			name:   "mise-node-multi-version",
			plugin: "mise-node",
			config: map[string]any{
				"version":        "22",
				"extra_versions": []string{"20"},
			},
			checkCmd:   "node --version",
			wantSubstr: "v22.",
			extraChecks: []struct {
				cmd        string
				wantSubstr string
			}{
				{"mise where node@20", ".local/share/mise"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newBootstrapHarness(t)
			h.Install(tc.plugin, tc.config)
			h.AssertCommand(tc.checkCmd, tc.wantSubstr)
			for _, ec := range tc.extraChecks {
				h.AssertCommand(ec.cmd, ec.wantSubstr)
			}
		})
	}
}
