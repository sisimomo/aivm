package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestFlow_DestroyKeepBase_ThenStart_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Destroy(ctx, true); err != nil {
		t.Fatal(err)
	}
	h.VM().ResetCallLog()
	h.SetVMStatus(vm.StatusNotFound)

	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast start after destroy --keep-base")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at must survive destroy --keep-base + fast start")
	}
	if !h.StateFileExists("bootstrap-state.json") {
		t.Fatal("bootstrap state must survive destroy --keep-base")
	}
}

func TestFlow_DestroyKeepBase_ThenRecreateFast(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Destroy(ctx, true); err != nil {
		t.Fatal(err)
	}
	h.VM().ResetCallLog()
	h.SetVMStatus(vm.StatusNotFound)

	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate after destroy --keep-base")
	}
}

func TestFlow_FullDestroy_ThenStart_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Destroy(ctx, false); err != nil {
		t.Fatal(err)
	}
	h.VM().ResetCallLog()
	h.SetVMStatus(vm.StatusNotFound)

	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("full destroy clears base; start must full bootstrap")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap on start after full destroy")
	}
}
