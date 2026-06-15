package vm_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/mountspec"
	"github.com/sisimomo/aivm/internal/vm"
)

type recordingDocker struct {
	calls            [][]string
	failTaggedCommit bool
}

func (r *recordingDocker) exec(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) >= 2 && args[0] == "commit" {
		if r.failTaggedCommit && len(args) == 3 {
			return "", errors.New("save failed")
		}
		return "sha256:ephemeral", nil
	}
	return "", nil
}

func dockerArgs(calls [][]string, index int) []string {
	if index >= len(calls) {
		return nil
	}
	return calls[index]
}

func mountCountFromRun(args []string) int {
	count := 0
	for i := 0; i < len(args); i++ {
		if args[i] == "-v" && i+1 < len(args) {
			count++
			i++
		}
	}
	return count
}

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

	rec := &recordingDocker{}
	restore := vm.SetDockerExecHookForTest(rec.exec)
	t.Cleanup(restore)

	d := vm.NewDocker("p", state, "img", true)
	runtimeOpts := vm.StartOptions{Mounts: runtimeMounts}

	if err := d.FinalizeAfterBootstrap(context.Background(), runtimeOpts); err != nil {
		t.Fatalf("FinalizeAfterBootstrap: %v", err)
	}

	wantTag := vm.DockerBaseImageTag("p")
	if len(rec.calls) < 4 {
		t.Fatalf("calls = %#v, want commit/stop/rm/run sequence", rec.calls)
	}
	if got := strings.Join(rec.calls[0], " "); got != fmt.Sprintf("commit p %s", wantTag) {
		t.Fatalf("first call = %q, want tagged commit", got)
	}
	if got := strings.Join(rec.calls[1], " "); got != "stop p" {
		t.Fatalf("second call = %q, want stop", got)
	}
	if got := strings.Join(rec.calls[2], " "); got != "rm -f p" {
		t.Fatalf("third call = %q, want rm", got)
	}
	runArgs := dockerArgs(rec.calls, 3)
	if runArgs == nil || runArgs[0] != "run" {
		t.Fatalf("fourth call = %#v, want docker run", runArgs)
	}
	if got := mountCountFromRun(runArgs); got != len(runtimeMounts) {
		t.Fatalf("restore mount count = %d, want runtime %d", got, len(runtimeMounts))
	}
	if got := mountCountFromRun(runArgs); got <= len(bootstrapMounts) {
		t.Fatalf("restore mount count = %d, want > bootstrap %d", got, len(bootstrapMounts))
	}
	for _, call := range rec.calls {
		if call[0] == "commit" && len(call) == 2 {
			t.Fatalf("unexpected ephemeral commit during save/restore path: %#v", rec.calls)
		}
	}
}

func TestDockerVM_FinalizeAfterBootstrap_SaveFailureFallsBack(t *testing.T) {
	rec := &recordingDocker{failTaggedCommit: true}
	restore := vm.SetDockerExecHookForTest(rec.exec)
	t.Cleanup(restore)

	d := vm.NewDocker("p", t.TempDir(), "img", true)
	runtimeOpts := vm.StartOptions{Mounts: []vm.Mount{{HostPath: "/h", GuestPath: "/g"}}}

	if err := d.FinalizeAfterBootstrap(context.Background(), runtimeOpts); err != nil {
		t.Fatalf("FinalizeAfterBootstrap: %v", err)
	}

	wantTag := vm.DockerBaseImageTag("p")
	if len(rec.calls) < 5 {
		t.Fatalf("calls = %#v, want save failure then ephemeral promote", rec.calls)
	}
	if got := strings.Join(rec.calls[0], " "); got != fmt.Sprintf("commit p %s", wantTag) {
		t.Fatalf("first call = %q, want tagged commit", got)
	}
	if got := strings.Join(rec.calls[1], " "); got != "commit p" {
		t.Fatalf("second call = %q, want ephemeral commit", got)
	}
	if got := strings.Join(rec.calls[2], " "); got != "stop p" {
		t.Fatalf("third call = %q, want stop", got)
	}
	if got := strings.Join(rec.calls[3], " "); got != "rm -f p" {
		t.Fatalf("fourth call = %q, want rm", got)
	}
	runArgs := dockerArgs(rec.calls, 4)
	if runArgs == nil || runArgs[0] != "run" {
		t.Fatalf("fifth call = %#v, want docker run", runArgs)
	}
	if got := strings.Join(rec.calls[len(rec.calls)-1], " "); got != "rmi -f sha256:ephemeral" {
		t.Fatalf("last call = %q, want ephemeral image cleanup", strings.Join(rec.calls[len(rec.calls)-1], " "))
	}
	for _, call := range rec.calls {
		if call[0] == "run" && len(call) > 0 {
			for _, arg := range call {
				if arg == wantTag {
					t.Fatalf("restore should not run on save failure, calls = %#v", rec.calls)
				}
			}
		}
	}
}
