package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aivm/internal/agent"
	"aivm/internal/config"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

func bootstrapEnabledPlugins(reg *plugin.Registry, provider agent.Provider, configured []string) []string {
	enabled := make([]string, 0, len(configured)+len(provider.RequiredPlugins()))
	seen := make(map[string]bool, len(configured)+len(provider.RequiredPlugins()))

	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		enabled = append(enabled, name)
	}

	for _, name := range configured {
		add(name)
	}
	for _, name := range provider.RequiredPlugins() {
		add(name)
	}
	return enabled
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func vmCreatedRecently(stateDir string) bool {
	data, err := os.ReadFile(filepath.Join(stateDir, "vm-created-at"))
	if err != nil {
		return false
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(epoch, 0)) < 10*time.Minute
}

// buildStartOptions constructs consistent vm.StartOptions from config.
// All VM-creating operations use this to eliminate duplication.
func buildStartOptions(cfg *config.Config) vm.StartOptions {
	mounts := make([]vm.Mount, 0, len(cfg.VM.ParsedMounts)+1)
	for _, m := range cfg.VM.ParsedMounts {
		mounts = append(mounts, vm.Mount{HostPath: m.HostPath, Writable: m.Writable})
	}
	mounts = append(mounts, vm.Mount{HostPath: filepath.Join(cfg.StateDir, ".claude", "projects"), Writable: true})
	return vm.StartOptions{
		CPUs:        cfg.VM.CPUs,
		MemoryBytes: cfg.VM.MemoryBytes,
		DiskBytes:   cfg.VM.DiskBytes,
		VMType:      cfg.VM.Type,
		Mounts:      mounts,
	}
}

// startFreshVM starts v using config-derived options and waits until ready.
// Use for all VM creation and rebuild operations.
func startFreshVM(ctx context.Context, v vm.VM, cfg *config.Config) error {
	os.MkdirAll(filepath.Join(cfg.StateDir, ".claude", "projects"), 0755)
	opts := buildStartOptions(cfg)
	if err := v.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}
	if err := v.WaitReady(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("waiting for VM: %w", err)
	}
	return nil
}
