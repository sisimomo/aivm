package vm_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/mountspec"
	"github.com/sisimomo/aivm/internal/vm"
)

type finalizeStubVM struct {
	baseImageEnable bool
	calls           []string
	saveMounts      int
	restoreMounts   int
}

func (s *finalizeStubVM) Profile() string { return "test" }

func (s *finalizeStubVM) NeedsPortBindingAtBoot() bool { return true }

func (s *finalizeStubVM) Status(_ context.Context) (vm.Status, error) {
	return vm.StatusRunning, nil
}

func (s *finalizeStubVM) Start(_ context.Context, _ vm.StartOptions) error { return nil }

func (s *finalizeStubVM) Stop(_ context.Context) error { return nil }

func (s *finalizeStubVM) Destroy(_ context.Context) error { return nil }

func (s *finalizeStubVM) Run(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (s *finalizeStubVM) RunOutput(_ context.Context, _ string, _ map[string]string) (string, error) {
	return "", nil
}

func (s *finalizeStubVM) RunInteractive(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (s *finalizeStubVM) RunStream(_ context.Context, _ string, _ map[string]string) (int, error) {
	return 0, nil
}

func (s *finalizeStubVM) SSH(_ context.Context, _ string, _ map[string]string) error { return nil }

func (s *finalizeStubVM) CopyTo(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *finalizeStubVM) CopyFrom(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *finalizeStubVM) WaitReady(_ context.Context, _ time.Duration) error { return nil }

func (s *finalizeStubVM) GetPublishedPort(_ int) (int, error) { return 0, nil }

func (s *finalizeStubVM) UsesBootstrapOnlyMounts() bool { return true }

func (s *finalizeStubVM) PrepareHostMountDir(_ string) error { return nil }

func (s *finalizeStubVM) AfterBootstrapPlugins(_ context.Context) error { return nil }

func (s *finalizeStubVM) FinalizeAfterBootstrap(_ context.Context, opts vm.StartOptions) error {
	if s.baseImageEnable {
		if err := s.SaveBaseImage(context.Background(), opts); err != nil {
			return err
		}
		return s.RestoreFromBaseImage(context.Background(), opts)
	}
	return nil
}

func (s *finalizeStubVM) SaveBaseImage(_ context.Context, opts vm.StartOptions) error {
	s.calls = append(s.calls, "SaveBaseImage")
	s.saveMounts = len(opts.Mounts)
	return nil
}

func (s *finalizeStubVM) RestoreFromBaseImage(_ context.Context, opts vm.StartOptions) error {
	s.calls = append(s.calls, "RestoreFromBaseImage")
	s.restoreMounts = len(opts.Mounts)
	return nil
}

func (s *finalizeStubVM) DeleteBaseImage(_ context.Context) error { return nil }

func (s *finalizeStubVM) HasBaseImage(_ context.Context) bool { return true }

func TestDockerVM_FinalizeAfterBootstrap_SaveAndRestore(t *testing.T) {
	home := "/Users/you"
	vmHome := "/home/user"
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".aivm")

	cfg := &config.Config{
		StateDir: state,
		VM: config.VMConfig{
			Backend:         "docker",
			BaseImageEnable: true,
			ParsedVMHome:    vmHome,
			ParsedMounts: []config.Mount{{
				HostPath:  filepath.Join(home, "dev"),
				GuestPath: filepath.Join(vmHome, "dev"),
				Writable:  true,
			}},
		},
	}
	agentDefs := map[string]agent.Def{
		"claude": {Mounts: []mountspec.MountSpec{{
			Source: "{{ .state_dir }}/.claude/projects",
			Target: "~/.claude/projects",
			Mode:   "rw",
		}}},
	}

	bootstrapMounts, err := lifecycle.ResolvedMountsForBootstrap(cfg, agentDefs, false)
	if err != nil {
		t.Fatalf("ResolvedMountsForBootstrap: %v", err)
	}
	runtimeMounts, err := lifecycle.ResolvedMountsForRuntime(cfg, agentDefs, false)
	if err != nil {
		t.Fatalf("ResolvedMountsForRuntime: %v", err)
	}
	if len(runtimeMounts) <= len(bootstrapMounts) {
		t.Fatalf("test setup: runtime mounts (%d) must exceed bootstrap (%d)",
			len(runtimeMounts), len(bootstrapMounts))
	}

	stub := &finalizeStubVM{baseImageEnable: true}
	runtimeOpts := vm.StartOptions{Mounts: runtimeMounts}

	if err := stub.FinalizeAfterBootstrap(context.Background(), runtimeOpts); err != nil {
		t.Fatalf("FinalizeAfterBootstrap: %v", err)
	}

	if len(stub.calls) != 2 {
		t.Fatalf("calls = %v, want [SaveBaseImage RestoreFromBaseImage]", stub.calls)
	}
	if stub.calls[0] != "SaveBaseImage" || stub.calls[1] != "RestoreFromBaseImage" {
		t.Fatalf("call order = %v, want Save before Restore", stub.calls)
	}
	if stub.saveMounts != len(runtimeMounts) {
		t.Fatalf("save mounts = %d, want runtime %d", stub.saveMounts, len(runtimeMounts))
	}
	if stub.restoreMounts <= len(bootstrapMounts) {
		t.Fatalf("restore mounts = %d, want > bootstrap %d", stub.restoreMounts, len(bootstrapMounts))
	}
	if stub.restoreMounts != len(runtimeMounts) {
		t.Fatalf("restore mounts = %d, want runtime %d", stub.restoreMounts, len(runtimeMounts))
	}
}
