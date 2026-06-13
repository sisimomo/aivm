package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestRecreate_FastIgnoredWhenBaseImageDisabled(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithBaseImageEnabled(false))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("--fast must be ignored when base images disabled")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap")
	}
}

func TestStart_BaseImageDisabled_MissingVM_NoSaveBaseImage(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithBaseImageEnabled(false))
	h.SetVMStatus(vm.StatusNotFound)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("SaveBaseImage") {
		t.Fatal("must not save base image when feature disabled")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap path")
	}
}

func TestStart_BaseImageDisabled_WithArtifact_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithBaseImageEnabled(false))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("must not restore when base images disabled")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap")
	}
}
