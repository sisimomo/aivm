package lifecycle_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestGuestPathForHost_Identity(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{{
		HostPath: "/Users/you/dev", GuestPath: "/Users/you/dev", Writable: true,
	}}}}
	got, err := lifecycle.GuestPathForHost("/Users/you/dev/myapp", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/Users/you/dev/myapp" {
		t.Fatalf("got %q", got)
	}
}

func TestGuestPathForHost_Remapped(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{{
		HostPath: "/Users/you/secrets", GuestPath: "/secrets", Writable: true,
	}}}}
	got, err := lifecycle.GuestPathForHost("/Users/you/secrets/keys", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/secrets/keys" {
		t.Fatalf("got %q", got)
	}
}

func TestGuestPathForHost_LongestPrefix(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{ParsedMounts: []config.Mount{
		{HostPath: "/Users/you", GuestPath: "/Users/you", Writable: true},
		{HostPath: "/Users/you/dev", GuestPath: "/work", Writable: true},
	}}}
	got, err := lifecycle.GuestPathForHost("/Users/you/dev/pkg", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/work/pkg" {
		t.Fatalf("got %q", got)
	}
}
