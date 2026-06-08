package log_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

func TestWithGroupPrefix(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	logger := loggerWithGroup("compose")
	logger.Info("started")

	out := buf.String()
	if !strings.Contains(out, "[compose] started") {
		t.Fatalf("want group prefix, got %q", out)
	}
}

func TestComponentAttrFormatting(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	slog.Info("output", slog.String("component", "claude"))

	out := buf.String()
	if !strings.Contains(out, "[claude] output") {
		t.Fatalf("want component tag, got %q", out)
	}
}

func TestWarnRoutesToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	configure(&out, &errBuf, aivmlog.LevelInfo)
	slog.Warn("warn message")
	if !strings.Contains(errBuf.String(), "warn message") {
		t.Fatalf("want warn on stderr, got %q", errBuf.String())
	}
	if strings.Contains(out.String(), "warn message") {
		t.Fatalf("warn should not appear on stdout, got %q", out.String())
	}
}

func TestNoColorDisablesANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	slog.Info("plain")
	if strings.Contains(buf.String(), "\033") {
		t.Fatalf("expected no ANSI escape codes, got %q", buf.String())
	}
}

func TestWithGroupConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}
	logger := loggerWithGroup("compose")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			logger.Info(fmt.Sprintf("msg-%d", n))
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(dir, "logs", "aivm.log"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != 20 {
		t.Fatalf("want 20 log lines, got %d:\n%s", len(lines), content)
	}
	for _, line := range lines {
		if !strings.Contains(line, "[compose] msg-") {
			t.Fatalf("malformed line: %q", line)
		}
	}
}

func TestTraceEnabledWhenFileAttached(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatal(err)
	}

	slog.Log(t.Context(), aivmlog.SlogTrace, "trace-enabled-check", slog.String("component", "vm"))
	if strings.Contains(buf.String(), "trace-enabled-check") {
		t.Fatal("trace should not appear on terminal at info level")
	}
	if !strings.Contains(readLogFile(t, dir, "aivm.log"), "[vm] trace-enabled-check") {
		t.Fatal("trace should be captured in log file when terminal is info")
	}
}
