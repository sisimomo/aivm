package integration_test

import (
	"testing"
	"text/template"

	"github.com/sisimomo/aivm/internal/integration"
)

// TestLoadDefaults_ParsesWithoutError ensures the embedded defaults.yaml is
// valid YAML and maps cleanly onto []IntegrationDef.
func TestLoadDefaults_ParsesWithoutError(t *testing.T) {
	_, err := integration.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
}

// TestLoadDefaults_ConfigureTemplatesCompile parses every configure script
// as a Go text/template to catch syntax errors before any container runs.
func TestLoadDefaults_ConfigureTemplatesCompile(t *testing.T) {
	defs, err := integration.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	for _, d := range defs {
		if d.Configure != "" {
			if _, err := template.New("").Parse(d.Configure); err != nil {
				t.Errorf("integration %q: configure is not a valid Go template: %v", d.Key(), err)
			}
		}
	}
}

// TestLoadDefaults_KeyDerivation verifies that IntegrationDef.Key() returns
// the Name field when set (as it is for all built-in integrations).
func TestLoadDefaults_KeyDerivation(t *testing.T) {
	defs, err := integration.LoadDefaults()
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
