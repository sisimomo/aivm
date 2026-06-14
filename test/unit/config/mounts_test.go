package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

func TestLoad_StructuredMounts(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	dir := t.TempDir()
	path := filepath.Join(dir, "aivm.yaml")
	content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  mounts:
    - source: "~/dev"
      target: "~/dev"
      mode: rw
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.VM.ParsedMounts) != 1 {
		t.Fatalf("ParsedMounts len = %d", len(cfg.VM.ParsedMounts))
	}
	m := cfg.VM.ParsedMounts[0]
	wantHost := filepath.Join(home, "dev")
	wantVM := filepath.Join(cfg.VM.ParsedVMHome, "dev")
	if m.HostPath != wantHost || m.GuestPath != wantVM || !m.Writable {
		t.Fatalf("got %+v, want host %q guest %q rw", m, wantHost, wantVM)
	}
}

func TestLoad_RejectsStringMount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "aivm.yaml")
	content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  mounts:
    - "~/dev:rw"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "vm.mounts") {
		t.Fatalf("expected vm.mounts error, got %v", err)
	}
}

func TestLoad_RejectsDuplicateTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "aivm.yaml")
	content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  mounts:
    - source: "~/dev"
      target: "~/dev"
      mode: rw
    - source: "~/other"
      target: "~/dev"
      mode: rw
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "duplicate target") {
		t.Fatalf("expected duplicate target error, got %v", err)
	}
}

func TestLoad_RejectsVMGuestHomeKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "aivm.yaml")
	content := `
agents:
  enabled: [claude]
vm:
  name: testvm
  guest_home: "/home/custom"
  mounts:
    - source: "~/dev"
      target: "~/dev"
      mode: rw
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path, config.Defaults{StateDir: "~/.aivm"})
	if err == nil || !strings.Contains(err.Error(), "vm.guest_home") {
		t.Fatalf("expected vm.guest_home error, got %v", err)
	}
}
