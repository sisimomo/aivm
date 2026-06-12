package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestStart_BootstrapRefreshAccepted_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithScriptedAnswers("y"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	oldBootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.BootstrapAtUnix() <= oldBootstrapAt {
		t.Fatal("expected bootstrap-at updated after accepted refresh")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap destroy")
	}
}

func TestStart_BootstrapRefreshDeclined_StoppedVM_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithScriptedAnswers("n"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("expected fast recreate when refresh declined on stopped VM")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at must be unchanged on fast recreate")
	}
}

func TestStart_CombinedPrompt_Option1_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("1"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("option 1 should full bootstrap")
	}
}

func TestStart_CombinedPrompt_Option2_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("2"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("option 2 should fast recreate")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at unchanged on fast recreate")
	}
}

func TestStart_CombinedPrompt_Option3_Resume(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("3"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") || h.VM().HasCall("Destroy") {
		t.Fatal("option 3 should resume without recreate")
	}
	if got, _ := h.VM().Status(ctx); got != vm.StatusRunning {
		t.Fatalf("expected VM running after resume, got %v", got)
	}
}

func TestStart_RuntimeChangeDeclined_PreservesBase(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBackend("docker"),
		harness.WithScriptedAnswers("n"),
	)
	h.SeedBootstrapStateWithBackend("lima", "qemu")
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("declined runtime change must not destroy VM")
	}
	if !h.HasBaseImage() {
		t.Fatal("base image must be preserved")
	}
}

func TestStart_RuntimeChangeAccepted_DeletesBaseAndRecreates(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBackend("docker"),
		harness.WithScriptedAnswers("y"),
	)
	h.SeedBootstrapStateWithBackend("lima", "qemu")
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("accepted runtime change must destroy VM")
	}
	if !h.VM().HasCall("DeleteBaseImage") {
		t.Fatal("accepted runtime change must delete base image before recreate")
	}
}

func TestStart_ConfigHashChange_DeletesBaseBeforePrompt(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithScriptedAnswers("n"))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)

	h.SVC().Config.VM.CPUs = 99

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.HasBaseImage() {
		t.Fatal("config change must delete base preemptively")
	}
}

func TestStart_ConfigHashChange_Accepted_RecreatesVM(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithScriptedAnswers("y"))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SVC().Config.VM.CPUs = 99

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("accepted config change must recreate VM")
	}
}

func TestStart_VMAgePromptAccepted_ValidBase_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("y"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(31)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("accepted VM age with valid base should fast recreate")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at unchanged on fast recreate")
	}
}
