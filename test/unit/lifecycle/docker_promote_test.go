package lifecycle_test

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

type promoteStubVM struct {
	calls []string
	saveMounts    int
	restoreMounts int
}

func (s *promoteStubVM) Profile() string { return "test" }

func (s *promoteStubVM) NeedsPortBindingAtBoot() bool { return true }

func (s *promoteStubVM) Status(_ context.Context) (vm.Status, error) {
	return vm.StatusRunning, nil
}

func (s *promoteStubVM) Start(_ context.Context, _ vm.StartOptions) error { return nil }

func (s *promoteStubVM) Stop(_ context.Context) error { return nil }

func (s *promoteStubVM) Destroy(_ context.Context) error { return nil }

func (s *promoteStubVM) Run(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (s *promoteStubVM) RunOutput(_ context.Context, _ string, _ map[string]string) (string, error) {
	return "", nil
}

func (s *promoteStubVM) RunInteractive(_ context.Context, _ string, _ map[string]string) error {
	return nil
}

func (s *promoteStubVM) RunStream(_ context.Context, _ string, _ map[string]string) (int, error) {
	return 0, nil
}

func (s *promoteStubVM) SSH(_ context.Context, _ string, _ map[string]string) error { return nil }

func (s *promoteStubVM) CopyTo(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *promoteStubVM) CopyFrom(_ context.Context, _, _ string, _ bool) error { return nil }

func (s *promoteStubVM) WaitReady(_ context.Context, _ time.Duration) error { return nil }

func (s *promoteStubVM) GetPublishedPort(_ int) (int, error) { return 0, nil }

func (s *promoteStubVM) UsesBootstrapOnlyMounts() bool { return true }

func (s *promoteStubVM) PrepareHostMountDir(_ string) error { return nil }

func (s *promoteStubVM) AfterBootstrapPlugins(_ context.Context) error { return nil }

func (s *promoteStubVM) FinalizeAfterBootstrap(_ context.Context, opts vm.StartOptions) error {
	if err := s.SaveBaseImage(context.Background(), opts); err != nil {
		return err
	}
	return s.RestoreFromBaseImage(context.Background(), opts)
}

func (s *promoteStubVM) SaveBaseImage(_ context.Context, opts vm.StartOptions) error {
	s.calls = append(s.calls, "SaveBaseImage")
	s.saveMounts = len(opts.Mounts)
	return nil
}

func (s *promoteStubVM) RestoreFromBaseImage(_ context.Context, opts vm.StartOptions) error {
	s.calls = append(s.calls, "RestoreFromBaseImage")
	s.restoreMounts = len(opts.Mounts)
	return nil
}

func (s *promoteStubVM) DeleteBaseImage(_ context.Context) error { return nil }

func (s *promoteStubVM) HasBaseImage(_ context.Context) bool { return true }

func TestPromoteDockerToRuntimeMounts_SaveAndRestore(t *testing.T) {
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

	stub := &promoteStubVM{}
	svc := &lifecycle.LifecycleService{
		Config:    cfg,
		VM:        stub,
		AgentDefs: agentDefs,
	}

	if err := lifecycle.PromoteDockerToRuntimeMountsForTest(context.Background(), svc); err != nil {
		t.Fatalf("promoteDockerToRuntimeMounts: %v", err)
	}

	if len(stub.calls) != 2 {
		t.Fatalf("calls = %v, want [SaveBaseImage RestoreFromBaseImage]", stub.calls)
	}
	if stub.calls[0] != "SaveBaseImage" || stub.calls[1] != "RestoreFromBaseImage" {
		t.Fatalf("call order = %v, want Save before Restore", stub.calls)
	}
	if stub.restoreMounts <= len(bootstrapMounts) {
		t.Fatalf("restore mounts = %d, want > bootstrap %d", stub.restoreMounts, len(bootstrapMounts))
	}
	if stub.restoreMounts != len(runtimeMounts) {
		t.Fatalf("restore mounts = %d, want runtime %d", stub.restoreMounts, len(runtimeMounts))
	}
}
