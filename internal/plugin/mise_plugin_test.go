package plugin

import (
	"context"
	"fmt"
	"testing"
)

type mockVMRunner struct {
	lastScript string
	err        error
}

func (m *mockVMRunner) Run(_ context.Context, script string, _ map[string]string) error {
	m.lastScript = script
	return m.err
}

func TestNewMisePlugin_Parsing(t *testing.T) {
	tests := []struct {
		input    string
		wantOK   bool
		wantTool string
	}{
		{"mise-java", true, "java"},
		{"mise-node", true, "node"},
		{"mise-go", true, "go"},
		{"mise-rust", true, "rust"},
		{"mise-", false, ""},   // empty tool
		{"java", false, ""},    // no prefix
		{"mise", false, ""},    // prefix only, no separator
		{"asdf-go", false, ""}, // different prefix
		{"", false, ""},        // empty string
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			p, ok := newMisePlugin(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("newMisePlugin(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			mp := p.(*misePlugin)
			if mp.tool != tc.wantTool {
				t.Errorf("tool=%q, want %q", mp.tool, tc.wantTool)
			}
			if p.Name() != tc.input {
				t.Errorf("Name()=%q, want %q", p.Name(), tc.input)
			}
			if p.Description() != tc.wantTool+" via mise" {
				t.Errorf("Description()=%q, want %q", p.Description(), tc.wantTool+" via mise")
			}
		})
	}
}

func TestMisePlugin_Dependencies(t *testing.T) {
	p, ok := newMisePlugin("mise-go")
	if !ok {
		t.Fatal("expected newMisePlugin to succeed")
	}
	deps := p.Dependencies()
	if len(deps) != 1 || deps[0] != "mise" {
		t.Errorf("Dependencies()=%v, want [mise]", deps)
	}
	if p.Agents() != nil {
		t.Errorf("Agents()=%v, want nil", p.Agents())
	}
	if p.PathEntries() != nil {
		t.Errorf("PathEntries()=%v, want nil", p.PathEntries())
	}
}

// wantSkipIfScript builds the expected SkipIf script for a tool + version list.
func wantSkipIfScript(tool string, versions ...string) string {
	header := fmt.Sprintf(
		"_check() {\n  local v=\"$1\"\n  if [ \"$v\" = \"latest\" ]; then\n    v=$(mise latest %s 2>/dev/null) || return 1\n  fi\n  mise where %s@\"$v\" >/dev/null 2>&1\n}\n",
		tool, tool)
	var checks string
	for i, v := range versions {
		if i > 0 {
			checks += " && "
		}
		checks += fmt.Sprintf("_check %q", v)
	}
	return header + checks
}

func TestMisePlugin_SkipIf_DefaultVersion(t *testing.T) {
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm}
	skip, err := p.SkipIf(context.Background(), env)
	if err != nil {
		t.Fatalf("SkipIf returned unexpected error: %v", err)
	}
	if !skip {
		t.Error("expected skip=true when VM.Run returns nil")
	}
	want := wantSkipIfScript("node", "latest")
	if vm.lastScript != want {
		t.Errorf("SkipIf script:\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

func TestMisePlugin_SkipIf_PinnedVersion(t *testing.T) {
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{"version": "22"}}
	skip, err := p.SkipIf(context.Background(), env)
	if err != nil {
		t.Fatalf("SkipIf returned unexpected error: %v", err)
	}
	if !skip {
		t.Error("expected skip=true when VM.Run returns nil")
	}
	want := wantSkipIfScript("node", "22")
	if vm.lastScript != want {
		t.Errorf("SkipIf script:\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

func TestMisePlugin_SkipIf_MultiVersion(t *testing.T) {
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{
		"version":        "22",
		"extra_versions": []string{"20", "18"},
	}}
	skip, err := p.SkipIf(context.Background(), env)
	if err != nil {
		t.Fatalf("SkipIf returned unexpected error: %v", err)
	}
	if !skip {
		t.Error("expected skip=true when VM.Run returns nil")
	}
	want := wantSkipIfScript("node", "22", "20", "18")
	if vm.lastScript != want {
		t.Errorf("SkipIf script:\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

func TestMisePlugin_SkipIf_MultiVersion_YAMLSlice(t *testing.T) {
	// Simulate YAML-unmarshalled []interface{} (as viper/mapstructure produces).
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{
		"version":        "22",
		"extra_versions": []interface{}{"20", "18"},
	}}
	_, err := p.SkipIf(context.Background(), env)
	if err != nil {
		t.Fatalf("SkipIf returned unexpected error: %v", err)
	}
	want := wantSkipIfScript("node", "22", "20", "18")
	if vm.lastScript != want {
		t.Errorf("SkipIf script ([]interface{} path):\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

func TestMisePlugin_SkipIf_NotInstalled(t *testing.T) {
	p, _ := newMisePlugin("mise-rust")
	vm := &mockVMRunner{err: errExitNonZero}

	env := InstallEnv{VM: vm}
	skip, err := p.SkipIf(context.Background(), env)
	if err != nil {
		t.Fatalf("SkipIf returned unexpected error: %v", err)
	}
	if skip {
		t.Error("expected skip=false when VM.Run returns error")
	}
}

func TestMisePlugin_Setup_DefaultVersion(t *testing.T) {
	p, _ := newMisePlugin("mise-go")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global go@latest"
	if vm.lastScript != want {
		t.Errorf("Setup script=%q, want %q", vm.lastScript, want)
	}
}

func TestMisePlugin_Setup_CustomVersion(t *testing.T) {
	p, _ := newMisePlugin("mise-java")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{"version": "21"}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global java@21"
	if vm.lastScript != want {
		t.Errorf("Setup script=%q, want %q", vm.lastScript, want)
	}
}

func TestMisePlugin_Setup_ExtraVersions(t *testing.T) {
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{
		"version":        "22",
		"extra_versions": []string{"20", "18"},
	}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global node@22\nmise install node@20\nmise install node@18"
	if vm.lastScript != want {
		t.Errorf("Setup script:\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

func TestMisePlugin_Setup_ExtraVersions_Latest(t *testing.T) {
	p, _ := newMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := InstallEnv{VM: vm, Config: map[string]any{
		"version":        "22",
		"extra_versions": []string{"latest", "20"},
	}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global node@22\nmise install node@latest\nmise install node@20"
	if vm.lastScript != want {
		t.Errorf("Setup script:\ngot:  %q\nwant: %q", vm.lastScript, want)
	}
}

// errExitNonZero simulates a non-zero exit from a VM script.
var errExitNonZero = &exitError{}

type exitError struct{}

func (e *exitError) Error() string { return "exit status 1" }
