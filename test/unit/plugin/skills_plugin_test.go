package plugin_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"github.com/sisimomo/aivm/internal/plugin"
)

func renderSkillsSetup(t *testing.T, data map[string]any) []string {
	t.Helper()
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	def, ok := defs["skills"]
	if !ok {
		t.Fatal("skills plugin not found in defaults.yaml")
	}
	tmpl, err := template.New("").Funcs(plugin.TemplateFuncMap()).Parse(def.Setup)
	if err != nil {
		t.Fatalf("template parse: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			lines = append(lines, s)
		}
	}
	return lines
}

func TestSkillsPlugin_Meta(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	def, ok := defs["skills"]
	if !ok {
		t.Fatal("skills plugin not found in defaults.yaml")
	}
	p := plugin.NewYAMLPlugin("skills", def)
	if p.Name() != "skills" {
		t.Errorf("Name()=%q, want %q", p.Name(), "skills")
	}
	if p.Description() == "" {
		t.Error("Description() must not be empty")
	}
	deps := p.Dependencies()
	if len(deps) != 1 || deps[0] != "mise-node" {
		t.Errorf("Dependencies()=%v, want [mise-node]", deps)
	}
	if len(p.Agents()) != 0 {
		t.Errorf("Agents()=%v, want empty", p.Agents())
	}
	if len(p.PathEntries()) != 0 {
		t.Errorf("PathEntries()=%v, want empty", p.PathEntries())
	}
}

func TestSkillsPlugin_NoSkipIf(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	def, ok := defs["skills"]
	if !ok {
		t.Fatal("skills plugin not found in defaults.yaml")
	}
	if def.SkipIf != "" {
		t.Errorf("SkipIf should be empty (always run), got %q", def.SkipIf)
	}
}

func TestSkillsPlugin_Setup_NoSources(t *testing.T) {
	lines := renderSkillsSetup(t, map[string]any{})
	if len(lines) != 0 {
		t.Errorf("expected no output for empty sources, got %v", lines)
	}
}

func TestSkillsPlugin_Setup_AllSkills(t *testing.T) {
	lines := renderSkillsSetup(t, map[string]any{
		"sources": []any{
			map[string]any{"repo": "mattpocock/skills"},
		},
	})
	want := []string{
		`npx skills@latest add "mattpocock/skills" --global --all`,
	}
	assertLines(t, lines, want)
}

func TestSkillsPlugin_Setup_SpecificSkills(t *testing.T) {
	lines := renderSkillsSetup(t, map[string]any{
		"sources": []any{
			map[string]any{
				"repo":   "twostraws/skills",
				"skills": []any{"tdd", "grill-me"},
			},
		},
	})
	want := []string{
		`npx skills@latest add "twostraws/skills" --global --yes --agent '*' --skill "tdd" --skill "grill-me"`,
	}
	assertLines(t, lines, want)
}

func TestSkillsPlugin_Setup_MultipleSources(t *testing.T) {
	lines := renderSkillsSetup(t, map[string]any{
		"sources": []any{
			map[string]any{"repo": "mattpocock/skills"},
			map[string]any{
				"repo":   "twostraws/skills",
				"skills": []any{"tdd"},
			},
		},
	})
	want := []string{
		`npx skills@latest add "mattpocock/skills" --global --all`,
		`npx skills@latest add "twostraws/skills" --global --yes --agent '*' --skill "tdd"`,
	}
	assertLines(t, lines, want)
}

func TestSkillsPlugin_Registry(t *testing.T) {
	defs, err := plugin.LoadDefaults()
	if err != nil {
		t.Fatalf("LoadDefaults: %v", err)
	}
	r := plugin.NewRegistry()
	for name, def := range defs {
		r.Set(plugin.NewYAMLPlugin(name, def))
	}
	p, ok := r.Get("skills")
	if !ok {
		t.Fatal("expected registry to resolve 'skills' after registering defaults")
	}
	if p.Name() != "skills" {
		t.Errorf("Name()=%q, want %q", p.Name(), "skills")
	}
	deps := p.Dependencies()
	if len(deps) != 1 || deps[0] != "mise-node" {
		t.Errorf("Dependencies()=%v, want [mise-node]", deps)
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\ngot:  %s\nwant: %s",
			len(got), len(want), fmt.Sprintf("%v", got), fmt.Sprintf("%v", want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\ngot:  %q\nwant: %q", i, got[i], want[i])
		}
	}
}

