package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

// TestPreservePluginsConfigCase verifies that uppercase keys nested inside
// plugins.config (e.g. env-var names) are not lowercased by Viper.
func TestPreservePluginsConfigCase(t *testing.T) {
	raw := `
plugins:
  config:
    cocoindex-code:
      variant: slim
      config:
        embedding:
          model: voyage/voyage-code-3
          provider: litellm
        envs:
          VOYAGE_API_KEY: my-secret-key
          count: 42
`
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "aivm.yaml")
	if err := os.WriteFile(cfgFile, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(cfgFile, config.Defaults{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}

	pluginCfg := cfg.Plugins.Config["cocoindex-code"]
	if pluginCfg == nil {
		t.Fatal("cocoindex-code config is nil")
	}

	configVal, ok := pluginCfg["config"]
	if !ok {
		t.Fatal("config key missing from plugin config")
	}
	nested, ok := configVal.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any for 'config', got %T", configVal)
	}

	envs, ok := nested["envs"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any for 'envs', got %T: %v", nested["envs"], nested["envs"])
	}

	if _, ok := envs["VOYAGE_API_KEY"]; !ok {
		t.Errorf("VOYAGE_API_KEY was lowercased to %q; case must be preserved", "voyage_api_key")
	}

	count, ok := envs["count"].(int)
	if !ok {
		t.Errorf("expected int for 'count', got %T: %v", envs["count"], envs["count"])
	} else if count != 42 {
		t.Errorf("expected count=42, got %d", count)
	}
}
