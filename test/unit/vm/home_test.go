package vm_test

import (
	"runtime"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestDefaultUserHome_Docker(t *testing.T) {
	t.Parallel()
	got := vm.DefaultUserHome("docker", "/Users/you")
	if got != "/home/user" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultUserHome_LimaLinux(t *testing.T) {
	t.Setenv("USER", "simon")
	got := vm.DefaultUserHome("lima", "/home/simon")
	if got != "/home/simon" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultUserHome_LimaDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	t.Setenv("USER", "simon")
	got := vm.DefaultUserHome("lima", "/Users/simon")
	if got != "/home/simon.guest" {
		t.Fatalf("got %q", got)
	}
}
