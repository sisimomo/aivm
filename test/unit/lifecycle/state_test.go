package lifecycle_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestBootstrapStateBackendFields(t *testing.T) {
	t.Parallel()
	s := &lifecycle.BootstrapState{
		Version:    lifecycle.BootstrapVersion,
		Backend:    "lima",
		VMType:     "vz",
		ConfigHash: "abc",
	}
	if s.NeedsMigration() {
		t.Fatal("current version should not need migration")
	}
}
