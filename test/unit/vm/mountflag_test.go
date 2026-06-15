package vm_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaMountYAML_SamePath(t *testing.T) {
	m := vm.Mount{
		HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true,
	}
	got := vm.LimaMountYAML(m)
	if !strings.Contains(got, `location: "/Users/you/dev"`) {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "mountPoint:") {
		t.Fatal("same absolute path should omit mountPoint")
	}
	if !strings.Contains(got, "writable: true") {
		t.Fatal("want writable: true")
	}
}

func TestLimaMountYAML_RemappedReadOnly(t *testing.T) {
	m := vm.Mount{
		HostPath: "/Users/you/secrets", GuestPath: "/secrets", Writable: false,
	}
	got := vm.LimaMountYAML(m)
	if !strings.Contains(got, `location: "/Users/you/secrets"`) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, `mountPoint: "/secrets"`) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "writable: false") {
		t.Fatal("want writable: false")
	}
}

func TestLimaMountsYAML_Multiple(t *testing.T) {
	got := vm.LimaMountsYAML([]vm.Mount{
		{HostPath: "/host", GuestPath: "/guest", Writable: true},
	})
	if !strings.HasPrefix(got, "mounts:\n") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, `mountPoint: "/guest"`) {
		t.Fatalf("got %q", got)
	}
}

func TestDockerVolumeFlag_Remapped(t *testing.T) {
	m := vm.Mount{HostPath: "/host", GuestPath: "/guest", Writable: true}
	got := vm.DockerVolumeFlag(m)
	want := "/host:/guest:rw"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
