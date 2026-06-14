package mountspec_test

import (
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
