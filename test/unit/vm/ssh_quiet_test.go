package vm_test

import (
	"bytes"
	"testing"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/vm"
)

func TestOpenSSHOptionsToolMode(t *testing.T) {
	orig := aivmlog.GetLevel()
	t.Cleanup(func() { aivmlog.SetLevel(orig) })

	aivmlog.SetLevel(aivmlog.LevelInfo)
	if got := vm.OpenSSHOptions(); got != nil {
		t.Fatalf("info level: want nil options, got %v", got)
	}

	aivmlog.SetLevel(aivmlog.LevelError)
	got := vm.OpenSSHOptions()
	if len(got) == 0 {
		t.Fatal("error level: want ssh -o flags")
	}
}

func TestQuietSSHLine(t *testing.T) {
	t.Parallel()
	if !vm.IsBenignSSHStderrLine("Shared connection to 127.0.0.1 closed.") {
		t.Fatal("expected match")
	}
	if vm.IsBenignSSHStderrLine("ssh: connect to host … Connection refused") {
		t.Fatal("real errors must not be filtered")
	}
}

func TestQuietStderrFiltersSharedConnection(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	w := vm.NewQuietStderr(&out)
	if _, err := w.Write([]byte("Shared connection to 127.0.0.1 closed.\nagent output\n")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "agent output\n" {
		t.Fatalf("got %q", got)
	}
}

func TestQuietStderrFlushTail(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	w := vm.NewQuietStderr(&out)
	if _, err := w.Write([]byte("connection refused")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("before flush: got %q", got)
	}
	w.Flush()
	if got := out.String(); got != "connection refused" {
		t.Fatalf("after flush: got %q", got)
	}
}
