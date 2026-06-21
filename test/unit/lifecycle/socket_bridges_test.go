package lifecycle_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/mountspec"
	"github.com/sisimomo/aivm/internal/vm"
)

func TestBuildRuntimeStartOptions_IncludesSocketBridges(t *testing.T) {
	home := "/Users/you"
	t.Setenv("HOME", home)
	cfg := &config.Config{
		StateDir: filepath.Join(home, ".aivm"),
		VM: config.VMConfig{
			Backend:      "docker",
			ParsedVMHome: "/home/user",
		},
		ParsedSocketBridges: []config.SocketBridge{{
			HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
		}},
	}
	opts, err := lifecycle.BuildRuntimeStartOptionsForTest(vm.NewDocker("p",
		cfg.StateDir, "img", false), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.SocketBridges) != 1 {
		t.Fatalf("SocketBridges len = %d", len(opts.SocketBridges))
	}
	if opts.SocketBridges[0].GuestPath != "/run/aivm/sockets/host.sock" {
		t.Fatalf("got %+v", opts.SocketBridges[0])
	}
}

func TestBuildBootstrapStartOptions_ExcludesSocketBridges(t *testing.T) {
	cfg := &config.Config{
		ParsedSocketBridges: []config.SocketBridge{{
			HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
		}},
	}
	opts, err := lifecycle.BuildBootstrapStartOptionsForTest(vm.NewDocker("p",
		t.TempDir(), "img", false), cfg, map[string]agent.Def{})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.SocketBridges) != 0 {
		t.Fatalf("bootstrap should not attach bridges, got %+v", opts.SocketBridges)
	}
}

func startUnixSocket(t *testing.T, path string) {
	t.Helper()
	_ = os.Remove(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close(); os.Remove(path) })
}

func TestValidateSocketBridgesForStart_UsesRuntimeMounts(t *testing.T) {
	home := "/Users/you"
	vmHome := "/home/you"
	t.Setenv("HOME", home)
	state := filepath.Join(home, ".aivm")
	agentGuest := filepath.Join(vmHome, ".claude/projects")
	dir := t.TempDir()
	sock := filepath.Join(dir, "ok.sock")
	startUnixSocket(t, sock)

	cfg := &config.Config{
		StateDir: state,
		VM: config.VMConfig{
			Backend:      "docker",
			ParsedVMHome: vmHome,
			ParsedMounts: []config.Mount{{
				HostPath:  filepath.Join(home, "dev"),
				GuestPath: filepath.Join(vmHome, "dev"),
				Writable:  true,
			}},
		},
		ParsedSocketBridges: []config.SocketBridge{{
			HostPath:  sock,
			GuestPath: agentGuest,
		}},
	}
	agentDefs := map[string]agent.Def{
		"claude": {Mounts: []mountspec.MountSpec{{
			Source: "{{ .state_dir }}/.claude/projects",
			Target: "~/.claude/projects",
			Mode:   "rw",
		}}},
	}

	err := lifecycle.ValidateSocketBridgesForTest(cfg, agentDefs)
	if err == nil {
		t.Fatal("expected guest_path conflict with agent mount")
	}
}
