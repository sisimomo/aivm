package mountspec_test

import (
	"runtime"
	"testing"

	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestDefaultGuestHome_Docker(t *testing.T) {
	t.Parallel()
	got := mountspec.DefaultGuestHome("docker", "/Users/you")
	if got != "/home/user" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultGuestHome_LimaLinux(t *testing.T) {
	t.Setenv("USER", "simon")
	got := mountspec.DefaultGuestHome("lima", "/home/simon")
	if got != "/home/simon" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultGuestHome_LimaDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	t.Setenv("USER", "simon")
	got := mountspec.DefaultGuestHome("lima", "/Users/simon")
	if got != "/home/simon.guest" {
		t.Fatalf("got %q", got)
	}
}
