package mountspec_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestValidateDuplicateMountPoint(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/a", GuestPath: "/x", Writable: true},
		{HostPath: "/b", GuestPath: "/x", Writable: true},
	}
	err := mountspec.ValidateDuplicateMountPoints(mounts)
	if err == nil || !strings.Contains(err.Error(), "/x") {
		t.Fatalf("expected duplicate mountPoint error, got %v", err)
	}
}

func TestValidateOverlappingLocations(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
		{HostPath: "/Users/you/dev/sub", GuestPath: "/sub", Writable: true},
	}
	err := mountspec.ValidateOverlappingLocations(mounts)
	if err == nil {
		t.Fatal("expected overlapping location error")
	}
}

func TestValidateOverlappingLocations_SiblingsOK(t *testing.T) {
	t.Parallel()
	mounts := []mountspec.ResolvedMount{
		{HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true},
		{HostPath: "/Users/you/other", GuestPath: "/Users/you/other", Writable: true},
	}
	if err := mountspec.ValidateOverlappingLocations(mounts); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
