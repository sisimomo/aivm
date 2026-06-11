package vm_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestBootstrapAtRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	vm.RecordBootstrapAt(dir)
	data, err := os.ReadFile(filepath.Join(dir, vm.BootstrapAtFile))
	if err != nil {
		t.Fatal(err)
	}
	epoch, _ := strconv.ParseInt(string(data), 10, 64)
	if time.Since(time.Unix(epoch, 0)) > time.Minute {
		t.Fatal("bootstrap-at should be recent")
	}
	vm.ClearHostAgeState(dir)
	if _, err := os.Stat(filepath.Join(dir, vm.BootstrapAtFile)); !os.IsNotExist(err) {
		t.Fatal("expected bootstrap-at removed")
	}
}
