package plugin_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"text/template"

	"github.com/sisimomo/aivm/internal/plugin"
	"gopkg.in/yaml.v3"
)

func renderTemplate(t *testing.T, src string, data any) string {
	t.Helper()
	tmpl, err := template.New("").Funcs(plugin.TemplateFuncMap()).Parse(src)
	if err != nil {
		t.Fatalf("template parse: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("template execute: %v", err)
	}
	return buf.String()
}

func TestTemplateFuncMap_toYAML(t *testing.T) {
	data := map[string]any{
		"embedding": map[string]any{
			"model":    "voyage/voyage-code-3",
			"provider": "litellm",
		},
		"envs": map[string]any{
			"VOYAGE_API_KEY": "secret",
		},
	}

	out := renderTemplate(t, `{{ . | toYAML }}`, data)

	// Result must be valid YAML that round-trips back to the original structure.
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("toYAML output is not valid YAML: %v\noutput:\n%s", err, out)
	}

	embedding, ok := decoded["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("embedding missing or wrong type after round-trip")
	}
	if embedding["model"] != "voyage/voyage-code-3" {
		t.Errorf("embedding.model = %q, want %q", embedding["model"], "voyage/voyage-code-3")
	}

	envs, ok := decoded["envs"].(map[string]any)
	if !ok {
		t.Fatalf("envs missing or wrong type after round-trip")
	}
	if envs["VOYAGE_API_KEY"] != "secret" {
		t.Errorf("envs.VOYAGE_API_KEY = %q, want %q", envs["VOYAGE_API_KEY"], "secret")
	}
}

func TestTemplateFuncMap_b64enc(t *testing.T) {
	out := renderTemplate(t, `{{ . | b64enc }}`, "hello world")

	want := base64.StdEncoding.EncodeToString([]byte("hello world"))
	if out != want {
		t.Errorf("b64enc(%q) = %q, want %q", "hello world", out, want)
	}
}

// TestTemplateFuncMap_ToYAMLThenB64enc exercises the exact pipeline used by the
// cocoindex-code plugin setup script: .config | toYAML | b64enc produces a
// base64 blob that decodes back to valid YAML matching the original config.
func TestTemplateFuncMap_ToYAMLThenB64enc(t *testing.T) {
	config := map[string]any{
		"embedding": map[string]any{"model": "voyage/voyage-code-3"},
		"envs":      map[string]any{"VOYAGE_API_KEY": "secret"},
	}

	out := renderTemplate(t, `{{ . | toYAML | b64enc }}`, config)

	// Must be pure base64 — safe to embed in a shell single-quoted string.
	if strings.ContainsAny(out, "'\"`$\\") {
		t.Errorf("b64enc output contains shell-special characters: %q", out)
	}

	decoded, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	var result map[string]any
	if err := yaml.Unmarshal(decoded, &result); err != nil {
		t.Fatalf("decoded YAML is invalid: %v\ncontent:\n%s", err, decoded)
	}

	embedding, ok := result["embedding"].(map[string]any)
	if !ok {
		t.Fatalf("embedding missing after decode+unmarshal")
	}
	if embedding["model"] != "voyage/voyage-code-3" {
		t.Errorf("embedding.model = %q after round-trip", embedding["model"])
	}
}
