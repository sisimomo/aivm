package vm_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

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

func TestValidateSocketBridgesForStart_DockerRequiresSocket(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sock")
	err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
		HostPath: missing, GuestPath: "/run/aivm/sockets/x.sock",
	}}, nil)
	if err == nil {
		t.Fatal("expected error for missing host socket")
	}
}

func TestValidateSocketBridgesForStart_DockerRejectsNonSocket(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
		HostPath: regularFile, GuestPath: "/run/aivm/sockets/x.sock",
	}}, nil)
	if err == nil {
		t.Fatal("expected error for non-socket host path")
	}
	if !strings.Contains(err.Error(), "Unix socket") {
		t.Fatalf("expected Unix socket error, got: %v", err)
	}
}

func TestValidateSocketBridgesForStart_DockerAcceptsSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ok.sock")
	startUnixSocket(t, sock)
	err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
		HostPath: sock, GuestPath: "/run/aivm/sockets/x.sock",
	}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSocketBridgesForStart_GuestConflictsWithMount(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ok.sock")
	startUnixSocket(t, sock)
	err := vm.ValidateSocketBridgesForStart("docker", []vm.SocketBridge{{
		HostPath: sock, GuestPath: "/work",
	}}, []vm.Mount{{HostPath: "/host", GuestPath: "/work", Writable: true}})
	if err == nil {
		t.Fatal("expected guest_path conflict error")
	}
}
