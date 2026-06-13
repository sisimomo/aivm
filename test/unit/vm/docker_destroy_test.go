package vm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestDockerVM_Destroy_PreservesHostAgeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agePath := filepath.Join(dir, vm.VMCreatedAtFile)
	if err := os.WriteFile(agePath, []byte("1700000000"), 0644); err != nil {
		t.Fatal(err)
	}

	d := vm.NewDocker("nosuch-container-"+t.Name(), dir, "ubuntu:24.04")
	if err := d.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agePath); err != nil {
		t.Fatalf("vm-created-at should survive Destroy: %v", err)
	}
}

func TestDockerVM_DestroyWithImages_PreservesHostAgeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agePath := filepath.Join(dir, vm.VMCreatedAtFile)
	if err := os.WriteFile(agePath, []byte("1700000000"), 0644); err != nil {
		t.Fatal(err)
	}

	d := vm.NewDocker("nosuch-container-"+t.Name(), dir, "ubuntu:24.04")
	d.DestroyWithImages()
	if _, err := os.Stat(agePath); err != nil {
		t.Fatalf("vm-created-at should survive DestroyWithImages: %v", err)
	}
}
