package vm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestDockerVM_UsesBootstrapOnlyMounts(t *testing.T) {
	t.Parallel()
	d := vm.NewDocker("p", t.TempDir(), "img", false)
	if !d.UsesBootstrapOnlyMounts() {
		t.Fatal("docker should use bootstrap-only mounts on first create")
	}
}

func TestDockerVM_PrepareHostMountDir_Chmods0777(t *testing.T) {
	t.Parallel()
	d := vm.NewDocker("p", t.TempDir(), "img", false)
	dir := filepath.Join(t.TempDir(), "persist")
	if err := d.PrepareHostMountDir(dir); err != nil {
		t.Fatalf("PrepareHostMountDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o777 {
		t.Fatalf("mode = %o, want 0777", info.Mode().Perm())
	}
}
