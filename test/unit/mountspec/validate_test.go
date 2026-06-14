package mountspec_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestValidateDuplicateTarget(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/a", GuestPath: "/x", Writable: true},
		{HostPath: "/b", GuestPath: "/x", Writable: true},
	}
	err := mountspec.ValidateDuplicateTargets(mounts)
	if err == nil {
		t.Fatalf("expected duplicate target error, got %v", err)
	}
}

func TestValidateOverlappingSources(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
		{HostPath: "/Users/you/dev/sub", GuestPath: "/sub", Writable: true},
	}
	err := mountspec.ValidateOverlappingSources(mounts)
	if err == nil {
		t.Fatal("expected overlapping source error")
	}
}

func TestValidateOverlappingSources_SiblingsOK(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
		{HostPath: "/Users/you/other", GuestPath: "/Users/you/other", Writable: true},
	}
	if err := mountspec.ValidateOverlappingSources(mounts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
