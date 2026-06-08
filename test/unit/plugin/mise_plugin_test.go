package plugin_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/plugin"
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
			p, ok := plugin.NewMisePlugin(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("NewMisePlugin(%q) ok=%v, want %v", tc.input, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			mp := p.(*plugin.MisePlugin)
			if mp.Tool != tc.wantTool {
				t.Errorf("Tool=%q, want %q", mp.Tool, tc.wantTool)
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
	p, ok := plugin.NewMisePlugin("mise-go")
	if !ok {
		t.Fatal("expected NewMisePlugin to succeed")
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

func TestMisePlugin_Setup_DefaultVersion(t *testing.T) {
	p, _ := plugin.NewMisePlugin("mise-go")
	vm := &mockVMRunner{}

	env := plugin.InstallEnv{VM: vm, Config: map[string]any{}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global go@latest"
	if vm.lastScript != want {
		t.Errorf("Setup script=%q, want %q", vm.lastScript, want)
	}
}

func TestMisePlugin_Setup_CustomVersion(t *testing.T) {
	p, _ := plugin.NewMisePlugin("mise-java")
	vm := &mockVMRunner{}

	env := plugin.InstallEnv{VM: vm, Config: map[string]any{"version": "21"}}
	if err := p.Setup(context.Background(), env); err != nil {
		t.Fatalf("Setup returned unexpected error: %v", err)
	}
	want := "mise use --global java@21"
	if vm.lastScript != want {
		t.Errorf("Setup script=%q, want %q", vm.lastScript, want)
	}
}

func TestMisePlugin_Setup_ExtraVersions(t *testing.T) {
	p, _ := plugin.NewMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := plugin.InstallEnv{VM: vm, Config: map[string]any{
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
	p, _ := plugin.NewMisePlugin("mise-node")
	vm := &mockVMRunner{}

	env := plugin.InstallEnv{VM: vm, Config: map[string]any{
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
