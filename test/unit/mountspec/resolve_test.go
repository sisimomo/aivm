package mountspec_test

import (
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolveMountSpec_IdentityWithTemplates(t *testing.T) {
	t.Parallel()
	home := "/Users/you"
	state := "/Users/you/.aivm"
	spec := mountspec.MountSpec{
		Location:   `{{ .home }}/dev`,
		MountPoint: `{{ .home }}/dev`,
		Mode:       "rw",
	}
	got, err := mountspec.Resolve(spec, mountspec.Context{Home: home, StateDir: state})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantHost := filepath.Join(home, "dev")
	if got.HostPath != wantHost || got.GuestPath != wantHost {
		t.Fatalf("got host=%q guest=%q, want %q", got.HostPath, got.GuestPath, wantHost)
	}
	if !got.Writable {
		t.Fatal("want writable")
	}
}

func TestResolveMountSpec_RemappedReadOnly(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Location:   "/Users/you/secrets",
		MountPoint: "/secrets",
		Mode:       "ro",
	}
	ctx := mountspec.Context{Home: "/Users/you", StateDir: "/Users/you/.aivm"}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.HostPath != "/Users/you/secrets" || got.GuestPath != "/secrets" {
		t.Fatalf("paths: host=%q guest=%q", got.HostPath, got.GuestPath)
	}
	if got.Writable {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveMountSpec_TildeExpansion(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Location:   "~/dev",
		MountPoint: "~/dev",
		Mode:       "rw",
	}
	ctx := mountspec.Context{Home: "/Users/you", StateDir: "/Users/you/.aivm"}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.HostPath != "/Users/you/dev" {
		t.Fatalf("HostPath = %q", got.HostPath)
	}
}

func TestResolveMountSpec_MissingField(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{MountPoint: "/x", Mode: "rw"}
	ctx := mountspec.Context{Home: "/h", StateDir: "/s"}
	_, err := mountspec.Resolve(spec, ctx)
	if err == nil {
		t.Fatal("expected error for missing location")
	}
}
