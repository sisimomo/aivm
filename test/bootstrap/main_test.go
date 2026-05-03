//go:build bootstrap

// Package bootstraptest contains Docker-based tests that exercise the full
// bootstrap pipeline — plugins, agents, and integrations — inside a real Ubuntu
// container. Tests run in parallel; each test gets its own fresh container.
//
// Run with:
//
//	make test-bootstrap
//
// or directly:
//
//	go test -tags bootstrap -v -timeout 120m ./test/bootstrap/
package bootstraptest

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	cleanupOrphanedTestDirs()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Fprintf(os.Stderr, "\n[bootstrap-test] caught %v — cleaning up...\n", sig)
		cleanupOrphanedTestDirs()
		os.Exit(1)
	}()

	code := m.Run()
	cleanupOrphanedTestDirs()
	os.Exit(code)
}

func cleanupOrphanedTestDirs() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := home + "/.aivm/test-runs"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "aivm-bstest-") {
			path := dir + "/" + e.Name()
			fmt.Fprintf(os.Stderr, "[bootstrap-test] removing orphaned dir: %s\n", path)
			os.RemoveAll(path) //nolint:errcheck
		}
	}
}
