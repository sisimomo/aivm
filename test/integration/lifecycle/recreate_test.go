package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
	"github.com/sisimomo/aivm/test/testvm"
)

func TestRecreate_FastWithValidBase_RestoresWithoutFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate via RestoreFromBaseImage")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at must not change on fast recreate")
	}
}

func TestRecreate_FastWithoutBase_FallsBackToFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(false)

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected full bootstrap, not restore")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected Destroy on full bootstrap path")
	}
}

func TestRecreate_RestoreFailure_FallsBackToFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.VM().SetFaults(testvm.Faults{RestoreFromBaseImageErr: errors.New("restore failed")})

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("expected invalid base deleted after restore failure")
	}
	if h.VM().CallCount("Destroy") < 1 {
		t.Fatal("expected full bootstrap destroy after restore failure")
	}
}

func TestRecreate_Default_FullBootstrapDespiteValidBase(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(10)
	oldBootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, false); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("recreate without --fast must not restore from base")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap destroy")
	}
	if h.BootstrapAtUnix() <= oldBootstrapAt {
		t.Fatal("full recreate must refresh bootstrap-at")
	}
}

func TestRecreate_Full_DeletesBaseBeforeBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, false); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("full bootstrap must delete existing base first")
	}
}

func TestRecreate_Fast_WaitReadyFailure_FallsBackToFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.VM().SetFaults(testvm.Faults{WaitReadyErr: errors.New("not ready")})

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("wait ready failure should invalidate base")
	}
	if h.VM().CallCount("Destroy") < 1 {
		t.Fatal("expected fallback full bootstrap")
	}
}
