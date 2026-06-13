package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestStart_InvalidConfigHash_MissingVM_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapConfigHash("stale-hash")

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("invalid config hash must not fast restore")
	}
	if !h.VM().HasCall("Start") {
		t.Fatal("expected fresh VM start on missing VM path")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("expected full bootstrap to save new base image")
	}
}

func TestStart_MissingBaseArtifact_MissingVM_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(false)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("missing base artifact must not restore")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap")
	}
}

func TestStart_VMTypeMismatchInState_MissingVM_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapVMType("mismatched-type")

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("vm_type mismatch must invalidate base for restore")
	}
	if !h.VM().HasCall("Start") {
		t.Fatal("expected fresh VM start when base is invalid")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("expected full bootstrap after invalid base")
	}
}

func TestStart_BackendMismatch_MissingVM_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapBackend("lima")

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("backend mismatch must not fast restore")
	}
	if !h.VM().HasCall("Start") {
		t.Fatal("expected fresh VM start when base is invalid")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("expected full bootstrap after invalid base")
	}
}

func TestStart_StaleBootstrapVersion_MissingVM_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapVersion("1")

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("stale bootstrap version must not fast restore")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("stale version on missing VM should full bootstrap")
	}
}

func TestStart_StaleBootstrapVersion_RunningVM_Recreates(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapVersion("1")

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("stale bootstrap version should delete base before recreate")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("stale bootstrap version should recreate VM")
	}
}
