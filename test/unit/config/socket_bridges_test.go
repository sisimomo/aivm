package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "aivm.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func minimalVMHeader() string {
	return `
agents:
  enabled: [claude]
vm:
  name: testvm
`
}

func TestLoad_SocketBridge_ExpandsHostPath(t *testing.T) {
	dir := t.TempDir()
	sockDir := filepath.Join(dir, "sockets")
	t.Setenv("TEST_SOCK_DIR", sockDir)
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "${TEST_SOCK_DIR}/service.sock"
    guest_path: /run/aivm/sockets/service.sock
`)
	cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SocketBridges) != 1 {
		t.Fatalf("len = %d", len(cfg.SocketBridges))
	}
	wantHost := filepath.Join(sockDir, "service.sock")
	b := cfg.SocketBridges[0]
	if b.HostPath != wantHost || b.GuestPath != "/run/aivm/sockets/service.sock" {
		t.Fatalf("got %+v, want host %q guest /run/aivm/sockets/service.sock", b, wantHost)
	}
}

func TestLoad_SocketBridge_TildeExpansion(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "~/.config/foo/service.sock"
    guest_path: /run/aivm/sockets/foo.sock
`)
	cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, ".config/foo/service.sock")
	if cfg.SocketBridges[0].HostPath != want {
		t.Fatalf("HostPath = %q, want %q", cfg.SocketBridges[0].HostPath, want)
	}
}

func TestLoad_SocketBridge_TemplateGuestPath(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "~/.config/herdr/herdr.sock"
    guest_path: "{{ .host_home }}/.config/herdr/herdr.sock"
`)
	cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(home, ".config/herdr/herdr.sock")
	b := cfg.SocketBridges[0]
	if b.HostPath != want || b.GuestPath != want {
		t.Fatalf("got host=%q guest=%q, want both %q", b.HostPath, b.GuestPath, want)
	}
}

func TestLoad_SocketBridge_TemplateHostPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "{{ .state_dir }}/service.sock"
    guest_path: /run/aivm/sockets/service.sock
`)
	cfg, err := config.Load(path, config.Defaults{StateDir: stateDir})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantHost := filepath.Join(stateDir, "service.sock")
	b := cfg.SocketBridges[0]
	if b.HostPath != wantHost || b.GuestPath != "/run/aivm/sockets/service.sock" {
		t.Fatalf("got %+v, want host %q", b, wantHost)
	}
}

func TestLoad_SocketBridge_RejectsRelativeGuestPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/sock"
    guest_path: run/sockets/foo.sock
`)
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "guest_path") {
		t.Fatalf("expected guest_path error, got %v", err)
	}
}

func TestLoad_SocketBridge_RejectsDuplicateGuestPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/a.sock"
    guest_path: /run/aivm/sockets/foo.sock
  - host_path: "/tmp/b.sock"
    guest_path: /run/aivm/sockets/foo.sock
`)
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "duplicate guest_path") {
		t.Fatalf("expected duplicate guest_path error, got %v", err)
	}
}

func TestLoad_SocketBridge_RejectsDuplicateHostPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader()+`
socket_bridges:
  - host_path: "/tmp/same.sock"
    guest_path: /run/aivm/sockets/a.sock
  - host_path: "/tmp/same.sock"
    guest_path: /run/aivm/sockets/b.sock
`)
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "duplicate host_path") {
		t.Fatalf("expected duplicate host_path error, got %v", err)
	}
}

func TestLoad_SocketBridge_EmptyListValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeConfig(t, dir, minimalVMHeader())
	cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SocketBridges) != 0 {
		t.Fatalf("expected no bridges, got %+v", cfg.SocketBridges)
	}
}
