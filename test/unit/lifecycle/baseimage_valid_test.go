package lifecycle_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestBaseImageValid_AllChecksPass(t *testing.T) {
	t.Parallel()
	state := &lifecycle.BootstrapState{
		Version:    lifecycle.BootstrapVersion,
		ConfigHash: "abc123",
		Backend:    "lima",
		VMType:     "vz",
	}
	check := lifecycle.BaseImageCheck{
		ConfigHash:     "abc123",
		Backend:        "lima",
		VMType:         "vz",
		ArtifactExists: true,
	}
	if !lifecycle.BaseImageValid(state, check) {
		t.Fatal("expected valid when all checks pass")
	}
}

func TestBaseImageValid_ConfigHashMismatch(t *testing.T) {
	t.Parallel()
	state := &lifecycle.BootstrapState{
		ConfigHash: "old",
		Backend:    "lima",
		VMType:     "vz",
	}
	check := lifecycle.BaseImageCheck{
		ConfigHash:     "new",
		Backend:        "lima",
		VMType:         "vz",
		ArtifactExists: true,
	}
	if lifecycle.BaseImageValid(state, check) {
		t.Fatal("expected invalid when config hash mismatches")
	}
}

func TestBaseImageValid_NilState(t *testing.T) {
	t.Parallel()
	check := lifecycle.BaseImageCheck{
		ConfigHash:     "abc123",
		Backend:        "lima",
		VMType:         "vz",
		ArtifactExists: true,
	}
	if lifecycle.BaseImageValid(nil, check) {
		t.Fatal("expected invalid when state is nil")
	}
}

func TestBaseImageValid_StaleBootstrapVersion(t *testing.T) {
	t.Parallel()
	state := &lifecycle.BootstrapState{
		Version:    "1",
		ConfigHash: "abc123",
		Backend:    "lima",
		VMType:     "vz",
	}
	check := lifecycle.BaseImageCheck{
		ConfigHash:     "abc123",
		Backend:        "lima",
		VMType:         "vz",
		ArtifactExists: true,
	}
	if lifecycle.BaseImageValid(state, check) {
		t.Fatal("expected invalid when bootstrap version is stale")
	}
}

func TestBaseImageValid_MissingArtifact(t *testing.T) {
	t.Parallel()
	state := &lifecycle.BootstrapState{
		ConfigHash: "abc123",
		Backend:    "lima",
		VMType:     "vz",
	}
	check := lifecycle.BaseImageCheck{
		ConfigHash:     "abc123",
		Backend:        "lima",
		VMType:         "vz",
		ArtifactExists: false,
	}
	if lifecycle.BaseImageValid(state, check) {
		t.Fatal("expected invalid when artifact is missing")
	}
}
