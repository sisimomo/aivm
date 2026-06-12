package vm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaVM_Destroy_PreservesHostAgeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agePath := filepath.Join(dir, vm.VMCreatedAtFile)
	if err := os.WriteFile(agePath, []byte("1700000000"), 0644); err != nil {
		t.Fatal(err)
	}

	l := vm.NewLima("nosuch-profile-"+t.Name(), dir)
	if err := l.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agePath); err != nil {
		t.Fatalf("vm-created-at should survive Destroy: %v", err)
	}
}
