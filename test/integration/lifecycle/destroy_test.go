package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestDestroy_KeepBase_PreservesBootstrapAndBaseImage(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Destroy(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !h.StateFileExists("bootstrap-state.json") {
		t.Fatal("expected bootstrap state preserved")
	}
	if !h.HasBaseImage() {
		t.Fatal("expected base image preserved")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected VM Destroy called")
	}
	if h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("DeleteBaseImage must not run when keepBase=true")
	}
	if !h.StateFileExists(vm.BootstrapAtFile) {
		t.Fatal("expected bootstrap-at preserved with --keep-base")
	}
	if !h.StateFileExists(vm.VMCreatedAtFile) {
		t.Fatal("expected vm-created-at preserved with --keep-base")
	}
}

func TestDestroy_NoKeepBase_ClearsBootstrapAndBaseImage(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Destroy(ctx, false); err != nil {
		t.Fatal(err)
	}
	if h.StateFileExists("bootstrap-state.json") {
		t.Fatal("expected bootstrap state cleared")
	}
	if h.HasBaseImage() {
		t.Fatal("expected base image deleted")
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("expected DeleteBaseImage called")
	}
	if h.StateFileExists(vm.BootstrapAtFile) {
		t.Fatal("expected bootstrap-at cleared")
	}
	if h.StateFileExists(vm.VMCreatedAtFile) {
		t.Fatal("expected vm-created-at cleared")
	}
}
