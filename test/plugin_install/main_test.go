//go:build plugin_install

// Package plugininstall contains integration tests that exercise each plugin's
// real install scripts inside a Docker container. Tests run in parallel — each
// plugin gets a fresh Ubuntu container. The full bootstrap engine is used so
// that template rendering and dependency resolution are also covered.
//
// Run with:
//
//	make test-plugin-install
//
// or directly:
//
//	go test -tags plugin_install -v -timeout 120m ./test/plugin_install/
package plugininstall

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
		fmt.Fprintf(os.Stderr, "\n[plugin-install-test] caught %v — cleaning up...\n", sig)
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
		if strings.HasPrefix(e.Name(), "aivm-pitest-") {
			path := dir + "/" + e.Name()
			fmt.Fprintf(os.Stderr, "[plugin-install-test] removing orphaned dir: %s\n", path)
			os.RemoveAll(path) //nolint:errcheck
		}
	}
}
