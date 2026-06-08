package config_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

func TestValidateVMConfig_RejectsColima(t *testing.T) {
	t.Parallel()

	path := writeYAML(t, `
agents:
  enabled:
    - claude
vm:
  backend: colima
`)

	_, err := config.Load(path, config.Defaults{StateDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for colima backend")
	}
	if !strings.Contains(err.Error(), "colima") {
		t.Fatalf("error = %q, want to contain colima", err.Error())
	}
}
