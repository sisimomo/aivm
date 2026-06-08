package vm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaSSHEndpoint_DefaultHome(t *testing.T) {
	t.Setenv("LIMA_HOME", "")
	home, _ := os.UserHomeDir()

	cfg, host := vm.LimaSSHEndpoint("aivm")
	want := filepath.Join(home, ".lima", "aivm", "ssh.config")
	if cfg != want {
		t.Fatalf("ssh config: got %q want %q", cfg, want)
	}
	if host != "lima-aivm" {
		t.Fatalf("ssh host: got %q want lima-aivm", host)
	}
}

func TestLimaSSHEndpoint_LimaHome(t *testing.T) {
	t.Setenv("LIMA_HOME", "/custom/lima")
	cfg, host := vm.LimaSSHEndpoint("dev")
	if cfg != "/custom/lima/dev/ssh.config" {
		t.Fatalf("ssh config: got %q", cfg)
	}
	if host != "lima-dev" {
		t.Fatalf("ssh host: got %q", host)
	}
}
