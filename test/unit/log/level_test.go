package log_test

import (
	"bytes"
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
		{"debug", aivmlog.LevelDebug, false},
		{"INFO", aivmlog.LevelInfo, false},
		{"warn", aivmlog.LevelWarn, false},
		{"error", aivmlog.LevelError, false},
		{"", aivmlog.LevelInfo, false},
		{"verbose", aivmlog.LevelInfo, true},
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
	l := aivmlog.NewWithLevel(&buf, &buf, aivmlog.LevelInfo)
	l.Info("shown")
	if !strings.Contains(buf.String(), "shown") {
		t.Fatalf("want info line, got %q", buf.String())
	}
}

func TestLevelErrorSuppressesProgress(t *testing.T) {
	var buf bytes.Buffer
	l := aivmlog.NewWithLevel(&buf, &buf, aivmlog.LevelError)
	l.Step("hidden")
	l.Success("hidden")
	l.Warn("hidden")
	l.Error("shown")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("unexpected progress output: %q", out)
	}
	if !strings.Contains(out, "shown") {
		t.Fatalf("want error line, got %q", out)
	}
}
