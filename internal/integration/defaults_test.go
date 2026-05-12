package integration

import (
	"strings"
	"testing"
	"text/template"
)

// TestLoadDefaults_ParsesWithoutError ensures the embedded defaults.yaml is
// valid YAML and maps cleanly onto []IntegrationDef.
func TestLoadDefaults_ParsesWithoutError(t *testing.T) {
	defs, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("LoadDefaults: returned empty list")
	}
}

// TestLoadDefaults_AllIntegrationsPresent validates that every built-in
// integration is declared with the correct To agent and non-empty scripts.
func TestLoadDefaults_AllIntegrationsPresent(t *testing.T) {
	defs, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	// Index by Key for easy lookup.
	byKey := make(map[string]IntegrationDef, len(defs))
	for _, d := range defs {
		byKey[d.Key()] = d
	}

	cases := []struct {
		key    string
		wantTo string
	}{
		{"mcpjungle:claude", "claude"},
		{"mcpjungle:copilot", "copilot"},
	}

	for _, tc := range cases {
		d, ok := byKey[tc.key]
		if !ok {
			t.Errorf("integration %q not found in defaults.yaml", tc.key)
			continue
		}
		if d.To != tc.wantTo {
			t.Errorf("integration %q: To=%q, want %q", tc.key, d.To, tc.wantTo)
		}
		if d.SkipIf == "" {
			t.Errorf("integration %q: empty skip_if", tc.key)
		}
		if d.Configure == "" {
			t.Errorf("integration %q: empty configure", tc.key)
		}
	}
}

// TestLoadDefaults_TemplatesCompile parses every skip_if and configure script
// as a Go text/template to catch syntax errors before any container runs.
func TestLoadDefaults_TemplatesCompile(t *testing.T) {
	defs, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	for _, d := range defs {
		if d.SkipIf != "" {
			if _, err := template.New("").Parse(d.SkipIf); err != nil {
				t.Errorf("integration %q: skip_if is not a valid Go template: %v", d.Key(), err)
			}
		}
		if d.Configure != "" {
			if _, err := template.New("").Parse(d.Configure); err != nil {
				t.Errorf("integration %q: configure is not a valid Go template: %v", d.Key(), err)
			}
		}
	}
}

// TestLoadDefaults_MCPJunglePorts validates that both mcpjungle integrations
// reference the .mcp_port template variable in both their skip_if guard and
// configure script. This prevents silent regressions where the port is
// hard-coded or the template placeholder is removed.
func TestLoadDefaults_MCPJunglePorts(t *testing.T) {
	defs, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	byKey := make(map[string]IntegrationDef, len(defs))
	for _, d := range defs {
		byKey[d.Key()] = d
	}

	for _, key := range []string{"mcpjungle:claude", "mcpjungle:copilot"} {
		d, ok := byKey[key]
		if !ok {
			t.Errorf("integration %q not found", key)
			continue
		}
		if !strings.Contains(d.SkipIf, ".mcp_port") {
			t.Errorf("integration %q: skip_if does not reference .mcp_port", key)
		}
		if !strings.Contains(d.Configure, ".mcp_port") {
			t.Errorf("integration %q: configure does not reference .mcp_port", key)
		}
	}
}

// TestLoadDefaults_KeyDerivation verifies that IntegrationDef.Key() returns
// the Name field when set (as it is for all built-in integrations).
func TestLoadDefaults_KeyDerivation(t *testing.T) {
	defs, err := LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	for _, d := range defs {
		if d.Name == "" {
			t.Errorf("built-in integration with To=%q has no Name; add a name field", d.To)
			continue
		}
		if d.Key() != d.Name {
			t.Errorf("integration Key()=%q, want Name=%q", d.Key(), d.Name)
		}
	}
}
