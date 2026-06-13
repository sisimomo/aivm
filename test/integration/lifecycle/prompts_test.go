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

func TestStart_BootstrapRefreshDeclined_RunningVM_NoRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithScriptedAnswers("n"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") || h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("declined refresh on running VM must keep VM as-is")
	}
}

func TestStart_VMAgePromptDeclined_ResumesStoppedVM(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("n"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SetVMCreatedDaysAgo(31)
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") || h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("declined VM age prompt must resume without recreate")
	}
	if got, _ := h.VM().Status(ctx); got != vm.StatusRunning {
		t.Fatalf("expected VM running after resume, got %v", got)
	}
}

func TestStart_CombinedPrompt_VMMissing_Option1_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("1"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("option 1 with missing VM should full bootstrap")
	}
}

func TestStart_CombinedPrompt_VMMissing_Option2_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
		harness.WithScriptedAnswers("2"),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("option 2 with missing VM should fast recreate")
	}
}

func TestStart_NonInteractive_StaleTimers_RunningVM_Resumes(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") || h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("non-interactive stale timers on running VM must resume silently")
	}
}

func TestStart_NonInteractive_StaleTimers_StoppedVM_Resumes(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
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
	if h.VM().HasCall("Destroy") || h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("non-interactive stale timers on stopped VM must resume silently")
	}
	if got, _ := h.VM().Status(ctx); got != vm.StatusRunning {
		t.Fatalf("expected VM started, got %v", got)
	}
}

func TestStart_NonInteractive_RuntimeMismatch_NoDestroy(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithBackend("docker"))
	h.SeedBootstrapStateWithBackend("lima", "qemu")
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("non-interactive runtime mismatch must not destroy VM")
	}
	if !h.HasBaseImage() {
		t.Fatal("base image must be preserved when runtime mismatch skipped")
	}
}

func TestStart_NonInteractive_ConfigHashChange_BaseDeleted_NoDestroy(t *testing.T) {
	t.Parallel()
	h := harness.New(t)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SVC().Config.VM.CPUs = 99
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.HasBaseImage() {
		t.Fatal("config hash change must delete base preemptively")
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("non-interactive config change must resume without recreate")
	}
}

func TestStart_NonInteractive_VMMissing_StaleTimers_FastRecreate(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithRecreatePromptDays(30),
	)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusNotFound)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	h.SetVMCreatedDaysAgo(31)

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("non-interactive missing VM with valid base should fast restore")
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

func TestLaunch_BootstrapRefreshAccepted_FullBootstrap(t *testing.T) {
	t.Parallel()
	h := harness.New(t,
		harness.WithBootstrapRefreshDays(30),
		harness.WithScriptedAnswers("y"),
	)
	workDir := t.TempDir()
	h.SetLaunchWorkDir(workDir)
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusRunning)
	h.SetBaseImage(true)
	h.SetBootstrapDaysAgo(31)
	oldBootstrapAt := h.BootstrapAtUnix()

	ctx := context.Background()
	if err := h.SVC().Launch(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if h.BootstrapAtUnix() <= oldBootstrapAt {
		t.Fatal("expected bootstrap-at updated after accepted refresh on launch")
	}
	if !h.VM().HasCall("Destroy") {
		t.Fatal("expected full bootstrap destroy during launch")
	}
}

func TestStart_ConfigHashChangeDeclined_StoppedVM_NoDoublePrompt(t *testing.T) {
	t.Parallel()
	h := harness.New(t, harness.WithScriptedAnswers("n"))
	h.SeedBootstrapped()
	h.SetVMStatus(vm.StatusStopped)
	h.SetBaseImage(true)
	h.SVC().Config.VM.CPUs = 99
	h.VM().ResetCallLog()

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if h.VM().HasCall("Destroy") {
		t.Fatal("declined config change must resume stopped VM without recreate")
	}
	if got, _ := h.VM().Status(ctx); got != vm.StatusRunning {
		t.Fatalf("expected VM running after resume, got %v", got)
	}
}

func TestStart_CombinedPrompt_Option2_TerminatesSessions(t *testing.T) {
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
	h.SeedSession("/tmp/work")
	if h.ActiveSessionCount() != 1 {
		t.Fatal("expected seeded session")
	}

	ctx := context.Background()
	if err := h.SVC().Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.VM().HasCall("RestoreFromBaseImage") {
		t.Fatal("option 2 should fast recreate")
	}
	if h.ActiveSessionCount() != 0 {
		t.Fatal("fast recreate must terminate active sessions")
	}
}
