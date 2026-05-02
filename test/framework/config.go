package framework

import (
	"path/filepath"
	"time"

	"aivm/internal/config"
)

// testConfig holds configuration for a test Harness. It uses small defaults
// suitable for tests (minimal VM resources, short idle timeouts).
type testConfig struct {
	CPUs          int
	MemoryGiB     int
	DiskGiB       int
	VMType        string
	DevRoot       string
	IdleTimeout   time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration
	Plugins       []string
}

func defaultTestConfig() testConfig {
	return testConfig{
		CPUs:          1,
		MemoryGiB:     2,
		DiskGiB:       10,
		VMType:        "vz",
		DevRoot:       "", // computed in New() as <testRunDir>/dev unless overridden
		IdleTimeout:   10 * time.Second,
		DeleteTimeout: 10 * time.Second,
		PollInterval:  1 * time.Second,
		Plugins:       []string{},
	}
}

// Option is a functional option for configuring a test Harness.
type Option func(*testConfig)

// WithCPUs sets the number of vCPUs for the test VM.
func WithCPUs(n int) Option { return func(c *testConfig) { c.CPUs = n } }

// WithMemoryGiB sets the RAM (GiB) for the test VM.
func WithMemoryGiB(n int) Option { return func(c *testConfig) { c.MemoryGiB = n } }

// WithDiskGiB sets the disk size (GiB) for the test VM.
func WithDiskGiB(n int) Option { return func(c *testConfig) { c.DiskGiB = n } }

// WithVMType overrides the VM hypervisor type (e.g. "vz", "qemu").
func WithVMType(t string) Option { return func(c *testConfig) { c.VMType = t } }

// WithDevRoot sets the dev root directory mounted into the VM.
func WithDevRoot(p string) Option { return func(c *testConfig) { c.DevRoot = p } }

// WithIdleTimeout sets the idle-stop timeout for the monitor.
func WithIdleTimeout(d time.Duration) Option { return func(c *testConfig) { c.IdleTimeout = d } }

// WithDeleteTimeout sets the suspension-delete timeout for the monitor.
func WithDeleteTimeout(d time.Duration) Option { return func(c *testConfig) { c.DeleteTimeout = d } }

// WithPollInterval sets how often the idle monitor polls VM status.
func WithPollInterval(d time.Duration) Option { return func(c *testConfig) { c.PollInterval = d } }

// WithPlugins sets the list of bootstrap plugins to enable for the test VM.
// Defaults to an empty list (no bootstrap). Provide plugin names to run
// bootstrap steps during the test.
func WithPlugins(names ...string) Option {
	return func(c *testConfig) { c.Plugins = names }
}

func buildTestConfig(profile, stateDir string, tc testConfig) *config.Config {
	return &config.Config{
		VM: config.VMConfig{
			CPUs:                tc.CPUs,
			MemoryGiB:           tc.MemoryGiB,
			DiskGiB:             tc.DiskGiB,
			Type:                tc.VMType,
			MaxAgeDays:          0,
			BaseImageMaxAgeDays: 0,
			DevRoot:             tc.DevRoot,
			Profile:             profile,
		},
		MCP: config.MCPConfig{
			Port:       19999, // unused — MCP is stubbed
			DataDir:    filepath.Join(stateDir, "mcpjungle-data"),
			ImageTag:   "latest",
			ServerMode: "development",
		},
		Idle: config.IdleConfig{
			Timeout:       tc.IdleTimeout,
			DeleteTimeout: tc.DeleteTimeout,
		},
		Agent: config.AgentConfig{
			Provider: "claude",
		},
		Plugins: config.PluginsConfig{
			Enabled: tc.Plugins,
		},
		StateDir: stateDir,
	}
}
