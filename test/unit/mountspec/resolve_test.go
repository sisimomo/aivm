package mountspec_test

import (
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolveMountSpec_IdentityWithTemplates(t *testing.T) {
	t.Parallel()
	home := "/Users/you"
	guestHome := "/home/you"
	state := "/Users/you/.aivm"
	spec := mountspec.MountSpec{
		Source: `{{ .home }}/dev`,
		Target: `~/dev`,
		Mode:   "rw",
	}
	ctx := mountspec.Context{Home: home, GuestHome: guestHome, StateDir: state}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantHost := filepath.Join(home, "dev")
	wantGuest := filepath.Join(guestHome, "dev")
	if got.HostPath != wantHost || got.GuestPath != wantGuest {
		t.Fatalf("got host=%q guest=%q, want host=%q guest=%q",
			got.HostPath, got.GuestPath, wantHost, wantGuest)
	}
	if !got.Writable {
		t.Fatal("want writable")
	}
}

func TestResolveMountSpec_RemappedReadOnly(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Source: "/Users/you/secrets",
		Target: "/secrets",
		Mode:   "ro",
	}
	ctx := mountspec.Context{
		Home: "/Users/you", GuestHome: "/home/you", StateDir: "/Users/you/.aivm",
	}
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

func TestResolveMountSpec_TildeExpansionSource(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Source: "~/dev",
		Target: "/work",
		Mode:   "rw",
	}
	ctx := mountspec.Context{
		Home: "/Users/you", GuestHome: "/home/you", StateDir: "/Users/you/.aivm",
	}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.HostPath != "/Users/you/dev" {
		t.Fatalf("HostPath = %q", got.HostPath)
	}
}

func TestResolveMountSpec_TildeExpansionTarget(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Source: "/Users/you/dev",
		Target: "~/.claude/projects",
		Mode:   "rw",
	}
	ctx := mountspec.Context{
		Home: "/Users/you", GuestHome: "/home/you", StateDir: "/Users/you/.aivm",
	}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.GuestPath != "/home/you/.claude/projects" {
		t.Fatalf("GuestPath = %q", got.GuestPath)
	}
}

func TestResolveMountSpec_MissingField(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{Target: "/x", Mode: "rw"}
	ctx := mountspec.Context{Home: "/h", GuestHome: "/g", StateDir: "/s"}
	_, err := mountspec.Resolve(spec, ctx)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}
