package vm_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestDockerVM_HasBaseImage(t *testing.T) {
	t.Parallel()
	d := vm.NewDocker("nosuch", t.TempDir(), "ubuntu:24.04")
	if d.HasBaseImage(context.Background()) {
		t.Fatal("want false for missing image")
	}
}
