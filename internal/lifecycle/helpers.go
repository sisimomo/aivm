package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/vm"
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

// ensureAgentPersistDirs creates the host-side directories that are mounted
// into the VM for persistence. Directories are driven by the active agent's
// Persist field so no code change is needed when adding a new agent.
func ensureAgentPersistDirs(cfg *config.Config, agentDef agent.Def) {
	for _, rel := range agentDef.Persist {
		_ = os.MkdirAll(filepath.Join(cfg.StateDir, rel), 0755)
	}
	if cfg.T3Code.Enable {
		_ = os.MkdirAll(filepath.Join(cfg.StateDir, ".t3"), 0755)
	}
}

// buildStartOptions constructs consistent vm.StartOptions from config.
// All VM-creating operations use this to eliminate duplication.
func buildStartOptions(cfg *config.Config, agentDef agent.Def) vm.StartOptions {
	mounts := make([]vm.Mount, 0, len(cfg.VM.ParsedMounts)+len(agentDef.Persist)+1)
	for _, m := range cfg.VM.ParsedMounts {
		mounts = append(mounts, vm.Mount{HostPath: m.HostPath, Writable: m.Writable})
	}
	for _, rel := range agentDef.Persist {
		mounts = append(mounts, vm.Mount{HostPath: filepath.Join(cfg.StateDir, rel), Writable: true})
	}
	if cfg.T3Code.Enable {
		mounts = append(mounts, vm.Mount{HostPath: filepath.Join(cfg.StateDir, ".t3"), Writable: true})
	}
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
func startFreshVM(ctx context.Context, v vm.VM, cfg *config.Config, agentDef agent.Def) error {
	ensureAgentPersistDirs(cfg, agentDef)
	opts := buildStartOptions(cfg, agentDef)
	if err := v.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}
	if err := v.WaitReady(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("waiting for VM: %w", err)
	}
	return nil
}
