package log_test

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

func configure(out, err io.Writer, level aivmlog.Level) {
	aivmlog.Configure(out, err, level)
}

func testSetupWithFile(t *testing.T, dir string, term aivmlog.Level) {
	t.Helper()
	var termBuf bytes.Buffer
	configure(&termBuf, &termBuf, term)
	if err := aivmlog.InitStateDir(dir); err != nil {
		t.Fatalf("InitStateDir: %v", err)
	}
}

func readLogFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "logs", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func flushWriter(w io.Writer) {
	if c, ok := w.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

func loggerWithGroup(group string) *slog.Logger {
	return slog.New(slog.Default().Handler().WithGroup(group))
}
