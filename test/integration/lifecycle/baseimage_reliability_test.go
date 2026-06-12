package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
	"github.com/sisimomo/aivm/test/testvm"
)

func TestStart_FullBootstrap_SaveBaseImageFailure_StillSucceeds(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SetVMStatus(vm.StatusNotFound)
	h.VM().SetFaults(testvm.Faults{SaveBaseImageErr: errors.New("disk full")})

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.StateFileExists("bootstrap-state.json") {
		t.Fatal("bootstrap must succeed even when base save fails")
	}
	if h.HasBaseImage() {
		t.Fatal("base artifact must not exist after save failure")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("expected save attempt")
	}
}

func TestFlow_SaveFailsThenMissingVM_FullBootstrapOnNextStart(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SetVMStatus(vm.StatusNotFound)
	h.VM().SetFaults(testvm.Faults{SaveBaseImageErr: errors.New("disk full")})

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.HasBaseImage() {
		t.Fatal("first bootstrap should not retain base after save failure")
	}

	if err := h.SVC().Destroy(ctx, true); err != nil {
		t.Fatal(err)
	}
	h.SetVMStatus(vm.StatusNotFound)
	h.VM().ResetCallLog()
	h.VM().SetFaults(testvm.Faults{})

	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("without base artifact start must not fast restore")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("second bootstrap should retry base save")
	}
	if !h.HasBaseImage() {
		t.Fatal("expected base image saved on second bootstrap")
	}
}

func TestStart_RestoreFailure_MissingVM_FallsBackToFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.VM().SetFaults(testvm.Faults{
		RestoreFromBaseImageErr: errors.New("clone failed"),
	})

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate attempt first")
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("restore failure must delete broken base")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap fallback destroy")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("expected full bootstrap to save fresh base")
	}
}

func TestStart_FastRecreate_WaitReadyFailure_FallsBackToFullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.VM().SetFaults(testvm.Faults{WaitReadyErr: errors.New("timeout")})

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate attempt")
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("wait-ready failure must delete base before fallback")
	}
	if h.VM().CallCount("Destroy") < 1 {
		t.Fatal("expected full bootstrap destroy on fallback")
	}
}

func TestStart_RunningVM_ValidBase_NoTimers_ResumesWithoutRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("running VM with fresh timers must not recreate")
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("running VM must resume without destroy")
	}
}
