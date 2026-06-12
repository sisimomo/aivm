package lifecycle_test

import (
	"context"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestFastRecreate_ResetsVMCreatedAt_PreservesBootstrapAt(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(10)
	oldBootstrapAt := h.BootstrapAtUnix()
	oldVMCreatedAt := h.VMCreatedAtUnix()

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if h.BootstrapAtUnix() != oldBootstrapAt {
		t.Fatal("fast recreate must preserve bootstrap-at")
	}
	if h.VMCreatedAtUnix() <= oldVMCreatedAt {
		t.Fatal("fast recreate must reset vm-created-at")
	}
	if time.Now().Unix()-h.VMCreatedAtUnix() > 5 {
		t.Fatal("vm-created-at should be recent after fast recreate")
	}
}

func TestFullBootstrap_UpdatesBootstrapAt_SavesBaseImage(t *testing.T) {
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
	if h.BootstrapAtUnix() <= oldBootstrapAt {
		t.Fatal("full bootstrap must update bootstrap-at")
	}
	if !h.VM().HasCall("SaveBaseImage") {
		t.Fatal("full bootstrap must save base image")
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("full recreate must not restore from base")
	}
}

func TestFastRecreate_AppliesPostRestoreEnv(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SVC().Config.VM.Env = map[string]string{"INTEGRATION_TEST": "1"}

	ctx := context.Background()
	if err := h.SVC().Recreate(ctx, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.VMCallLogHasRunSubstr("aivm-user-env.sh") {
		t.Fatal("fast recreate must reapply vm.env via post-restore")
	}
}
