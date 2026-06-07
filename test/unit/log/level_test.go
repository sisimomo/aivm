package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    aivmlog.Level
		wantErr bool
	}{
		{"trace", aivmlog.LevelTrace, false},
		{"debug", aivmlog.LevelDebug, false},
		{"INFO", aivmlog.LevelInfo, false},
		{"warn", aivmlog.LevelWarn, false},
		{"error", aivmlog.LevelError, false},
		{"", aivmlog.LevelInfo, false},
		{"verbose", aivmlog.LevelInvalid, true},
	}
	for _, tc := range tests {
		got, err := aivmlog.ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q): want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestResolveLevelPrecedence(t *testing.T) {
	t.Setenv("AIVM_LOG_LEVEL", "warn")

	got, err := aivmlog.ResolveLevel("error", true, "debug")
	if err != nil {
		t.Fatal(err)
	}
	if got != aivmlog.LevelError {
		t.Fatalf("flag: got %v want error", got)
	}

	got, err = aivmlog.ResolveLevel("", false, "debug")
	if err != nil {
		t.Fatal(err)
	}
	if got != aivmlog.LevelWarn {
		t.Fatalf("env: got %v want warn", got)
	}

	_ = os.Unsetenv("AIVM_LOG_LEVEL")
	got, err = aivmlog.ResolveLevel("", false, "debug")
	if err != nil {
		t.Fatal(err)
	}
	if got != aivmlog.LevelDebug {
		t.Fatalf("config: got %v want debug", got)
	}
}

func TestLevelInfoShowsInfo(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	slog.Info("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Fatalf("want info line, got %q", buf.String())
	}
}

func TestLevelInfoHidesDebug(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	slog.Debug("hidden")
	if strings.Contains(buf.String(), "hidden") {
		t.Fatalf("unexpected debug output: %q", buf.String())
	}
}

func TestLevelDebugShowsDebug(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelDebug)
	slog.Debug("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Fatalf("want debug line, got %q", buf.String())
	}
}

func TestLevelTraceShowsTrace(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelTrace)
	slog.Log(context.Background(), aivmlog.SlogTrace, "shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Fatalf("want trace line, got %q", buf.String())
	}
}

func TestLevelErrorSuppressesProgress(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelError)
	slog.Info("hidden milestone")
	slog.Warn("hidden warn")
	slog.Error("shown")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("unexpected progress output: %q", out)
	}
	if !strings.Contains(out, "shown") {
		t.Fatalf("want error line, got %q", out)
	}
}

func TestUnifiedFormat(t *testing.T) {
	var buf bytes.Buffer
	configure(&buf, &buf, aivmlog.LevelInfo)
	slog.Info("hello")
	out := buf.String()
	if !strings.Contains(out, "[aivm]") {
		t.Fatalf("want [aivm] prefix, got %q", out)
	}
	if !strings.Contains(out, "INFO") {
		t.Fatalf("want INFO label, got %q", out)
	}
}
