package vm_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaMountFlag_SamePath(t *testing.T) {
	m := vm.Mount{
		HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true,
	}
	flag := vm.LimaMountFlag(m)
	if !strings.Contains(flag, "source=/Users/you/dev") {
		t.Fatalf("flag = %q", flag)
	}
	if !strings.Contains(flag, "target=/Users/you/dev") {
		t.Fatalf("flag = %q", flag)
	}
	if strings.Contains(flag, "readonly") {
		t.Fatal("writable mount should not be readonly")
	}
}

func TestLimaMountFlag_RemappedReadOnly(t *testing.T) {
	m := vm.Mount{
		HostPath: "/Users/you/secrets", GuestPath: "/secrets", Writable: false,
	}
	flag := vm.LimaMountFlag(m)
	if !strings.Contains(flag, "source=/Users/you/secrets") {
		t.Fatalf("flag = %q", flag)
	}
	if !strings.Contains(flag, "target=/secrets") {
		t.Fatalf("flag = %q", flag)
	}
	if !strings.Contains(flag, "readonly") {
		t.Fatal("want readonly")
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
