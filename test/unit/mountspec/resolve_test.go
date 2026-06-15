package mountspec_test

import (
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolveMountSpec_SamePathWithTemplates(t *testing.T) {
	t.Parallel()
	home := "/Users/you"
	vmHome := "/home/you"
	state := "/Users/you/.aivm"
	spec := mountspec.MountSpec{
		Source: `{{ .host_home }}/dev`,
		Target: `~/dev`,
		Mode:   "rw",
	}
	ctx := mountspec.Context{Home: home, VMHome: vmHome, StateDir: state}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantHost := filepath.Join(home, "dev")
	wantVM := filepath.Join(vmHome, "dev")
	if got.HostPath != wantHost || got.GuestPath != wantVM {
		t.Fatalf("got host=%q guest=%q, want host=%q guest=%q",
			got.HostPath, got.GuestPath, wantHost, wantVM)
	}
	if !got.Writable {
		t.Fatal("want writable")
	}
}

func TestResolveMountSpec_LiteralHostPathTarget(t *testing.T) {
	t.Parallel()
	home := "/Users/you"
	vmHome := "/home/you.guest"
	spec := mountspec.MountSpec{
		Source: "~/dev",
		Target: `{{ .host_home }}/dev`,
		Mode:   "rw",
	}
	ctx := mountspec.Context{Home: home, VMHome: vmHome, StateDir: home + "/.aivm"}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := filepath.Join(home, "dev")
	if got.HostPath != want || got.GuestPath != want {
		t.Fatalf("got host=%q guest=%q, want both %q",
			got.HostPath, got.GuestPath, want)
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
		Home: "/Users/you", VMHome: "/home/you", StateDir: "/Users/you/.aivm",
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
		Home: "/Users/you", VMHome: "/home/you", StateDir: "/Users/you/.aivm",
	}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.HostPath != "/Users/you/dev" {
		t.Fatalf("HostPath = %q", got.HostPath)
	}
}

func TestResolveMountSpec_BareTilde(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Source: "/Users/you/dev",
		Target: "~",
		Mode:   "rw",
	}
	ctx := mountspec.Context{
		Home: "/Users/you", VMHome: "/home/you", StateDir: "/Users/you/.aivm",
	}
	got, err := mountspec.Resolve(spec, ctx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.GuestPath != "/home/you" {
		t.Fatalf("GuestPath = %q", got.GuestPath)
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
		Home: "/Users/you", VMHome: "/home/you", StateDir: "/Users/you/.aivm",
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
	ctx := mountspec.Context{Home: "/h", VMHome: "/g", StateDir: "/s"}
	_, err := mountspec.Resolve(spec, ctx)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestResolveMountSpec_UnknownTemplateKey(t *testing.T) {
	t.Parallel()
	spec := mountspec.MountSpec{
		Source: `{{ .guest_home }}/dev`,
		Target: "/work",
		Mode:   "rw",
	}
	ctx := mountspec.Context{Home: "/h", VMHome: "/g", StateDir: "/s"}
	_, err := mountspec.Resolve(spec, ctx)
	if err == nil {
		t.Fatal("expected error for unknown template key")
	}
}
