package agent_test

import (
	"reflect"
	"testing"
	"text/template"

	"github.com/sisimomo/aivm/internal/agent"
)

// TestLoadDefs_ParsesWithoutError ensures the embedded defaults.yaml is valid YAML
// and maps cleanly onto the Def struct.
func TestLoadDefs_ParsesWithoutError(t *testing.T) {
	defs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("LoadDefs: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("LoadDefs: returned empty map")
	}
}

// TestLoadDefs_AllAgentsPresent validates that every built-in agent is declared
// with the fields required for bootstrap and runtime launch.
func TestLoadDefs_AllAgentsPresent(t *testing.T) {
	defs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("LoadDefs: %v", err)
	}

	cases := []struct{ name string }{
		{"claude"},
		{"copilot"},
		{"cursor"},
		{"opencode"},
	}

	for _, tc := range cases {
		def, ok := defs[tc.name]
		if !ok {
			t.Errorf("agent %q not found in defaults.yaml", tc.name)
			continue
		}
		if def.Description == "" {
			t.Errorf("agent %q: empty description", tc.name)
		}
		if def.SkipIf == "" {
			t.Errorf("agent %q: empty skip_if", tc.name)
		}
		if def.Setup == "" {
			t.Errorf("agent %q: empty setup", tc.name)
		}
		if def.LaunchCommand == "" {
			t.Errorf("agent %q: empty launch_command", tc.name)
		}
	}
}

// TestLoadDefs_ScriptsAreValidTemplates checks that every skip_if and setup
// script parses as a valid Go text/template. This catches accidental breakage
// of template syntax (e.g. a stray {{ or }}) before any Docker container runs.
func TestLoadDefs_ScriptsAreValidTemplates(t *testing.T) {
	defs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("LoadDefs: %v", err)
	}

	for name, def := range defs {
		if def.SkipIf != "" {
			if _, err := template.New("").Parse(def.SkipIf); err != nil {
				t.Errorf("agent %q: skip_if is not a valid Go template: %v", name, err)
			}
		}
		if def.Setup != "" {
			if _, err := template.New("").Parse(def.Setup); err != nil {
				t.Errorf("agent %q: setup is not a valid Go template: %v", name, err)
			}
		}
	}
}

// TestLoadDefs_ToPluginDef verifies that ToPluginDef copies the bootstrap-relevant
// fields correctly. These fields drive the actual VM provisioning, so a
// mis-mapping would silently break bootstrap without any script error.
func TestLoadDefs_ToPluginDef(t *testing.T) {
	defs, err := agent.LoadDefs()
	if err != nil {
		t.Fatalf("LoadDefs: %v", err)
	}

	for name, def := range defs {
		pd := def.ToPluginDef()
		if pd.Description != def.Description {
			t.Errorf("agent %q: ToPluginDef().Description=%q, want %q", name, pd.Description, def.Description)
		}
		if pd.SkipIf != def.SkipIf {
			t.Errorf("agent %q: ToPluginDef().SkipIf mismatch", name)
		}
		if pd.Setup != def.Setup {
			t.Errorf("agent %q: ToPluginDef().Setup mismatch", name)
		}
		if !reflect.DeepEqual(pd.Dependencies, def.Dependencies) {
			t.Errorf("agent %q: ToPluginDef().Dependencies=%v, want %v", name, pd.Dependencies, def.Dependencies)
		}
		if !reflect.DeepEqual(pd.PathEntries, def.PathEntries) {
			t.Errorf("agent %q: ToPluginDef().PathEntries=%v, want %v", name, pd.PathEntries, def.PathEntries)
		}
	}
}
