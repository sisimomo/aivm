package e2e

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

// TestMain wraps the test run with orphaned state dir cleanup and a signal
// handler so that Ctrl+C also triggers cleanup.
func TestMain(m *testing.M) {
	cleanupOrphanedStateDirs()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Fprintf(os.Stderr, "\n[aivm-test] caught %v — cleaning up...\n", sig)
		cleanupOrphanedStateDirs()
		os.Exit(1)
	}()

	code := m.Run()
	cleanupOrphanedStateDirs()
	os.Exit(code)
}

// cleanupOrphanedStateDirs removes any ~/.aivm/test-runs/aivm-test-* directories
// left over from a crashed test run.
func cleanupOrphanedStateDirs() {
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
		if strings.HasPrefix(e.Name(), "aivm-test-") {
			path := dir + "/" + e.Name()
			fmt.Fprintf(os.Stderr, "[aivm-test] removing orphaned state dir: %s\n", path)
			os.RemoveAll(path) //nolint:errcheck
		}
	}
}
