package lifecycle_test

import (
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/mountspec"
)

func TestResolvedMountsForStart_DedupesAgentTarget(t *testing.T) {
	home := "/Users/you"
	guestHome := "/home/you"
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".aivm")
	cfg := &config.Config{
		StateDir: state,
		VM: config.VMConfig{
			ParsedGuestHome: guestHome,
			ParsedMounts: []config.Mount{{
				HostPath:  filepath.Join(home, "dev"),
				GuestPath: filepath.Join(guestHome, "dev"),
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
	mounts, err := lifecycle.ResolvedMountsForStart(cfg, agentDefs, false)
	if err != nil {
		t.Fatalf("ResolvedMountsForStart: %v", err)
	}
	// expect vm mount + 1 agent mount
	if len(mounts) != 2 {
		t.Fatalf("len = %d", len(mounts))
	}
	if mounts[1].GuestPath != filepath.Join(guestHome, ".claude/projects") {
		t.Fatalf("guest = %q", mounts[1].GuestPath)
	}
}
