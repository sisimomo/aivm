package config_test

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/config"
)

func TestLoad_BaseImageDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load("", config.Defaults{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.VM.BaseImageEnable {
		t.Fatal("want base_image_enable true by default")
	}
	if cfg.VM.BootstrapRefreshPromptAfterDuration != 30*24*time.Hour {
		t.Fatalf("bootstrap_refresh_prompt_after: got %v want 720h", cfg.VM.BootstrapRefreshPromptAfterDuration)
	}
}

func TestLoad_BaseImageDisabled(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `vm:
  base_image_enable: false
  bootstrap_refresh_prompt_after: "-1"
`)
	cfg, err := config.Load(path, config.Defaults{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VM.BaseImageEnable {
		t.Fatal("want base_image_enable false")
	}
	if cfg.VM.BootstrapRefreshPromptAfterDuration != config.DisabledDuration {
		t.Fatal("want disabled bootstrap refresh")
	}
}
