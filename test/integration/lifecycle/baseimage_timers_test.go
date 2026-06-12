package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/lifecycle/harness"
)

func TestStart_MissingBootstrapAt_SkipsBootstrapRefreshPrompt(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("y"),
	)
	h.SeedBootstrapped()
	h.RemoveBootstrapAt()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("missing bootstrap-at must not trigger bootstrap refresh")
	}
}

func TestStart_VMAgeOnly_VMMissing_Interactive_FastRecreateOnOption2(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("2"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(31)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("VM age due with missing VM should offer fast recreate")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at unchanged on fast recreate")
	}
}

func TestStart_BootstrapRefreshOnly_VMMissing_Declined_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithScriptedAnswers("n"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	bootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("declined bootstrap refresh on missing VM should fast restore")
	}
	if h.BootstrapAtUnix() != bootstrapAt {
		t.Fatal("bootstrap-at unchanged on fast recreate")
	}
}

func TestStart_TimersDisabled_Interactive_NoRecreation(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(-1),
		harness.WithRecreatePromptDays(-1),
		harness.WithScriptedAnswers("y"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(365)
	h.SetVMCreatedDaysAgo(365)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("disabled timers must not trigger recreation")
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("disabled timers must not destroy VM")
	}
}

func TestStart_NonInteractive_VMMissing_OnlyVMAgeDue_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithRecreatePromptDays(30))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("non-interactive missing VM should fast restore when base valid")
	}
}
