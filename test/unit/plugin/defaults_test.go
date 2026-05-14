package plugin_test

import (
	"strings"
	"testing"
	"text/template"

	"github.com/sisimomo/aivm/internal/plugin"
)

// TestLoadDefaults_ParsesWithoutError ensures the embedded defaults.yaml is
// valid YAML and maps cleanly onto the PluginDef struct.
func TestLoadDefaults_ParsesWithoutError(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("LoadDefaults: returned empty map")
	}
}

// TestLoadDefaults_AllPluginsPresent validates that every built-in plugin is
// declared with the fields required for bootstrap execution.
func TestLoadDefaults_AllPluginsPresent(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	cases := []struct{ name string }{
		{"system"},
		{"mise"},
		{"awscli"},
		{"t3code"},
	}

	for _, tc := range cases {
		def, ok := defs[tc.name]
		if !ok {
			t.Errorf("plugin %q not found in defaults.yaml", tc.name)
			continue
		}
		if def.Description == "" {
			t.Errorf("plugin %q: empty description", tc.name)
		}
		if def.SkipIf == "" {
			t.Errorf("plugin %q: empty skip_if", tc.name)
		}
		if def.Setup == "" {
			t.Errorf("plugin %q: empty setup", tc.name)
		}
	}
}

// TestLoadDefaults_MiseDependsOnSystem asserts that the mise plugin lists
// system as a dependency so baseline packages (curl, etc.) are guaranteed to
// be installed before mise's setup script runs.
func TestLoadDefaults_MiseDependsOnSystem(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	mise, ok := defs["mise"]
	if !ok {
		t.Fatal("mise plugin not found")
	}

	for _, dep := range mise.Dependencies {
		if dep == "system" {
			return
		}
	}
	t.Errorf("mise: 'system' not found in dependencies %v", mise.Dependencies)
}

// TestLoadDefaults_AWSCliIsArchAware checks that the awscli setup script
// handles both x86_64 and aarch64 architectures. A regression here would
// silently install the wrong binary on ARM hosts.
func TestLoadDefaults_AWSCliIsArchAware(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	awscli, ok := defs["awscli"]
	if !ok {
		t.Fatal("awscli plugin not found")
	}

	if !strings.Contains(awscli.Setup, "aarch64") {
		t.Error("awscli setup: missing aarch64 architecture check")
	}
	if !strings.Contains(awscli.Setup, "x86_64") {
		t.Error("awscli setup: missing x86_64 architecture check")
	}
}

// TestLoadDefaults_T3CodeUsesStateDirTemplate validates that the t3code setup
// script references the state_dir template variable. This is the host-mounted
// persistence path; if the reference is removed the bind-mount symlink is
// never created and state is lost on VM recreation.
func TestLoadDefaults_T3CodeUsesStateDirTemplate(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	t3code, ok := defs["t3code"]
	if !ok {
		t.Fatal("t3code plugin not found")
	}

	if !strings.Contains(t3code.Setup, ".state_dir") {
		t.Error("t3code setup: does not reference {{ .state_dir }} template variable")
	}
}

// TestLoadDefaults_ScriptsAreValidTemplates parses every skip_if and setup
// script as a Go text/template to catch syntax errors before any container runs.
func TestLoadDefaults_ScriptsAreValidTemplates(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}

	for name, def := range defs {
		if def.SkipIf != "" {
			if _, err := template.New("").Parse(def.SkipIf); err != nil {
				t.Errorf("plugin %q: skip_if is not a valid Go template: %v", name, err)
			}
		}
		if def.Setup != "" {
			if _, err := template.New("").Parse(def.Setup); err != nil {
				t.Errorf("plugin %q: setup is not a valid Go template: %v", name, err)
			}
		}
	}
}
