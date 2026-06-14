package mountspec_test

import (
	"runtime"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestDefaultVMHome_Docker(t *testing.T) {
	t.Parallel()
	got := mountspec.DefaultVMHome("docker", "/Users/you")
	if got != "/home/user" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultVMHome_LimaLinux(t *testing.T) {
	t.Setenv("USER", "simon")
	got := mountspec.DefaultVMHome("lima", "/home/simon")
	if got != "/home/simon" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultVMHome_LimaDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	t.Setenv("USER", "simon")
	got := mountspec.DefaultVMHome("lima", "/Users/simon")
	if got != "/home/simon.guest" {
		t.Fatalf("got %q", got)
	}
}
