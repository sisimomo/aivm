package lifecycle

import (
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

func TestPathUnderMount(t *testing.T) {
	t.Parallel()
	mount := "/mnt/proj"
	tests := []struct {
		path string
		want bool
	}{
		{"/mnt/proj", true},
		{"/mnt/proj/src", true},
		{"/mnt/proj2", false},
		{"/mnt/proj2/src", false},
	}
	for _, tc := range tests {
		if got := pathUnderMount(tc.path, mount); got != tc.want {
			t.Errorf("pathUnderMount(%q, %q) = %v, want %v", tc.path, mount, got, tc.want)
		}
	}

	root := "/"
	rootTests := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/home/user", true},
	}
	for _, tc := range rootTests {
		if got := pathUnderMount(tc.path, root); got != tc.want {
			t.Errorf("pathUnderMount(%q, %q) = %v, want %v", tc.path, root, got, tc.want)
		}
	}
}

func TestAssertUnderMountRejectsSiblingPath(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		VM: config.VMConfig{
			ParsedMounts: []config.Mount{
				{HostPath: "/mnt/proj"},
			},
		},
	}
	if err := assertUnderMount("/mnt/proj2", cfg); err == nil {
		t.Fatal("expected error for sibling path")
	}
}
