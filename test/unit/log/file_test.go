package log_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

func TestSubprocessWriterLineBuffering(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelTrace)

	w := aivmlog.Writer("colima")
	if _, err := w.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if content := readLogFile(t, dir, "aivm.log"); strings.Contains(content, "partial") {
		t.Fatalf("partial line should not be logged yet, got %q", content)
	}
	if _, err := w.Write([]byte(" line\n")); err != nil {
		t.Fatal(err)
	}
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "[colima] partial line") {
		t.Fatalf("want tagged line in file, got %q", content)
	}
}

func TestSubprocessWriterConcurrent(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelTrace)

	w := aivmlog.Writer("test")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = fmt.Fprintf(w, "line-%d\n", n)
		}(i)
	}
	wg.Wait()

	content := readLogFile(t, dir, "aivm.log")
	for i := range 20 {
		want := fmt.Sprintf("[test] line-%d", i)
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in log:\n%s", want, content)
		}
	}
}

func TestFileCapturesTraceWhenTerminalInfo(t *testing.T) {
	dir := t.TempDir()
	var termBuf bytes.Buffer
	configure(&termBuf, &termBuf, aivmlog.LevelInfo)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}

	slog.Log(context.Background(), aivmlog.SlogTrace, "file-only", slog.String("component", "vm"))
	if strings.Contains(termBuf.String(), "file-only") {
		t.Fatalf("trace should not appear on terminal at info level, got %q", termBuf.String())
	}
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "[vm] file-only") {
		t.Fatalf("want trace in file, got %q", content)
	}
}

func TestUseDedicatedLogClosesPreviousFile(t *testing.T) {
	dir := t.TempDir()
	var termBuf bytes.Buffer
	configure(&termBuf, &termBuf, aivmlog.LevelInfo)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}

	slog.Info("before switch")
	if err := aivmlog.UseDedicatedLog(dir, "idle-monitor"); err != nil {
		t.Fatal(err)
	}
	slog.Info("after switch")

	aivmContent := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(aivmContent, "before switch") {
		t.Fatalf("aivm.log missing first message: %q", aivmContent)
	}
	if strings.Contains(aivmContent, "after switch") {
		t.Fatalf("aivm.log should not contain post-switch message: %q", aivmContent)
	}

	monitorContent := readLogFile(t, dir, "idle-monitor.log")
	if !strings.Contains(monitorContent, "after switch") {
		t.Fatalf("idle-monitor.log missing second message: %q", monitorContent)
	}
}

func TestSubprocessWriterCloseFlushesPartialLine(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelTrace)

	w := aivmlog.Writer("colima")
	if _, err := w.Write([]byte("trailing without newline")); err != nil {
		t.Fatal(err)
	}
	if content := readLogFile(t, dir, "aivm.log"); strings.Contains(content, "trailing without newline") {
		t.Fatalf("partial line should not be logged before Close, got %q", content)
	}
	flushWriter(w)
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "[colima] trailing without newline") {
		t.Fatalf("want flushed line in file, got %q", content)
	}
}

func TestSubprocessWriterFileOnlyAtErrorLevel(t *testing.T) {
	dir := t.TempDir()
	var termBuf bytes.Buffer
	configure(&termBuf, &termBuf, aivmlog.LevelError)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}

	w := aivmlog.Writer("colima")
	if _, err := w.Write([]byte("subprocess output\n")); err != nil {
		t.Fatal(err)
	}
	flushWriter(w)
	if strings.Contains(termBuf.String(), "subprocess output") {
		t.Fatalf("subprocess output should not appear on terminal at error level, got %q", termBuf.String())
	}
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "[colima] subprocess output") {
		t.Fatalf("want subprocess output in file, got %q", content)
	}
}

func TestLogRotation(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "aivm.log")
	if err := os.WriteFile(logPath, make([]byte, 10*1024*1024), 0600); err != nil {
		t.Fatal(err)
	}

	var termBuf bytes.Buffer
	configure(&termBuf, &termBuf, aivmlog.LevelInfo)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}
	slog.Info("after rotation")

	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Fatalf("expected rotated backup file: %v", err)
	}
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "after rotation") {
		t.Fatalf("want new log content, got %q", content)
	}
}

func TestLogRotationOnWrite(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "aivm.log")
	const maxSize = 10 * 1024 * 1024
	if err := os.WriteFile(logPath, make([]byte, maxSize-100), 0600); err != nil {
		t.Fatal(err)
	}

	testSetupWithFile(t, dir, aivmlog.LevelInfo)
	// First write crosses the size limit; rotation runs on the next write.
	slog.Info(strings.Repeat("x", 500))
	slog.Info("trigger rotation")

	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Fatalf("expected rotated backup after writes exceeded threshold: %v", err)
	}
}

func TestSubprocessWriterCapsLongLine(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelTrace)

	w := aivmlog.Writer("colima")
	long := strings.Repeat("a", 300*1024)
	if _, err := w.Write([]byte(long)); err != nil {
		t.Fatal(err)
	}
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "[colima]") {
		t.Fatalf("want flushed oversized line in file, got %q", content)
	}
}

func TestFileSinkReopensAfterMissingFile(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelInfo)
	slog.Info("before delete")

	logPath := filepath.Join(dir, "logs", "aivm.log")
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}

	slog.Info("after delete")
	content := readLogFile(t, dir, "aivm.log")
	if !strings.Contains(content, "after delete") {
		t.Fatalf("want log written after file removed, got %q", content)
	}
}

func TestLogFilePermissions(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelInfo)
	slog.Info("perm check")

	info, err := os.Stat(filepath.Join(dir, "logs", "aivm.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("want log file mode 0600, got %o", info.Mode().Perm())
	}
}

func TestHandlerConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	testSetupWithFile(t, dir, aivmlog.LevelInfo)

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			slog.Info(fmt.Sprintf("msg-%d", n))
		}(i)
	}
	wg.Wait()

	content := readLogFile(t, dir, "aivm.log")
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != 50 {
		t.Fatalf("want 50 log lines, got %d:\n%s", len(lines), content)
	}
	for _, line := range lines {
		if !strings.Contains(line, " INFO  msg-") {
			t.Fatalf("malformed line: %q", line)
		}
	}
}
