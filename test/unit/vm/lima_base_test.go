package vm_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaFastRestoreArgs(t *testing.T) {
	t.Parallel()
	args := vm.LimaFastRestoreArgs("aivm-base", "aivm", 4, 8, 60, "vz")
	wantContains := []string{"clone", "aivm-base", "aivm", "--cpus", "4", "--memory", "8", "--disk", "60", "--vm-type", "vz", "--start"}
	for _, w := range wantContains {
		if !containsAll(args, w) {
			t.Fatalf("args %v missing %q", args, w)
		}
	}
	if containsAll(args, "--mount") {
		t.Fatalf("restore clone must not pass --mount (shadow already has mounts): %v", args)
	}
}

func containsAll(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
