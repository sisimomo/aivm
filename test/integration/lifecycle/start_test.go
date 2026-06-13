package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestStart_NonInteractive_VMMissing_ValidBase_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t) // SilentConfirmer default
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate when VM missing and base valid")
	}
}

func TestStart_NonInteractive_VMMissing_NoBase_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(false)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected full bootstrap, not restore")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap destroy path")
	}
	if !h.StateFileExists("bootstrap-state.json") {
		t.Fatal("expected fresh bootstrap state written")
	}
}
