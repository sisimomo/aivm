package vm_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/vmfactory"
)

func TestNewFromConfig_Lima(t *testing.T) {
	cfg := &config.VMConfig{Backend: "lima", Name: "aivm"}
	inst, err := vmfactory.NewFromConfig(cfg, "/tmp/state")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Profile() != "aivm" {
		t.Fatalf("profile: got %q", inst.Profile())
	}
}

func TestNewFromConfig_RejectsColima(t *testing.T) {
	cfg := &config.VMConfig{Backend: "colima", Name: "aivm"}
	_, err := vmfactory.NewFromConfig(cfg, "/tmp/state")
	if err == nil {
		t.Fatal("expected error for colima backend")
	}
}
