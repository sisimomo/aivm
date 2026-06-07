package lifecycle_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestLogsUnknownService(t *testing.T) {
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{StateDir: t.TempDir()},
	}
	err := svc.Logs("compose")
	if err == nil {
		t.Fatal("expected error for unknown log target")
	}
	if !strings.Contains(err.Error(), "unknown log") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogsMissingFile(t *testing.T) {
	dir := t.TempDir()
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{StateDir: dir},
	}
	err := svc.Logs("aivm")
	if err == nil {
		t.Fatal("expected error for missing log file")
	}
	if !strings.Contains(err.Error(), "log file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogsMonitorMissingFile(t *testing.T) {
	dir := t.TempDir()
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{StateDir: dir},
	}
	err := svc.Logs("monitor")
	if err == nil {
		t.Fatal("expected error for missing monitor log file")
	}
	want := filepath.Join(dir, "logs", "idle-monitor.log")
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("want path %q in error, got %v", want, err)
	}
}
