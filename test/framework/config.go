package framework

import (
	"path/filepath"
	"time"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
)

// testConfig holds configuration for a test Harness. It uses small defaults
// suitable for tests (minimal VM resources, short idle timeouts).
type testConfig struct {
	CPUs          int
	MemoryBytes   int64
	DiskBytes     int64
	VMType        string
	// DevRoot is a convenience field: if set, a single rw ParsedMount is created.
	DevRoot       string
	IdleTimeout   time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration
	Plugins       []string
	// RecreatePromptAfter sets VM.RecreatePromptAfterDuration.
	// Use config.DisabledDuration to disable. Zero means use default (disabled).
	RecreatePromptAfter time.Duration
	// BaseImageRebuildPromptAfter sets VM.BaseImageRebuildPromptAfterDuration.
	// Use config.DisabledDuration to disable. Zero means use default (disabled).
	BaseImageRebuildPromptAfter time.Duration
	// Provider selects the AI agent provider name (default "claude").
	Provider string
	// Integrations is an optional list of additional integrations to include alongside
	// the built-in test stubs.
	Integrations []integration.IntegrationDef
	// Interactive, when true, sets App.IsTerminal=true so interactive code paths run.
	Interactive bool
	// StdinAnswers is fed to App.Stdin, one answer per prompt (newline-separated).
	StdinAnswers []string
}

func defaultTestConfig() testConfig {
	return testConfig{
		CPUs:          1,
		MemoryBytes:   2 << 30, // 2 GiB
		DiskBytes:     10 << 30, // 10 GiB
		VMType:        "vz",
		DevRoot:       "", // computed in New() as <testRunDir>/dev unless overridden
		IdleTimeout:   10 * time.Second,
		DeleteTimeout: 10 * time.Second,
		PollInterval:  1 * time.Second,
		Plugins:       []string{},
		Provider:      "claude",
	}
}

// Option is a functional option for configuring a test Harness.
type Option func(*testConfig)

// WithCPUs sets the number of vCPUs for the test VM.
func WithCPUs(n int) Option { return func(c *testConfig) { c.CPUs = n } }

// WithMemoryGiB sets the RAM (GiB) for the test VM.
func WithMemoryGiB(n int) Option { return func(c *testConfig) { c.MemoryBytes = int64(n) << 30 } }

// WithDiskGiB sets the disk size (GiB) for the test VM.
func WithDiskGiB(n int) Option { return func(c *testConfig) { c.DiskBytes = int64(n) << 30 } }

// WithVMType overrides the VM hypervisor type (e.g. "vz", "qemu").
func WithVMType(t string) Option { return func(c *testConfig) { c.VMType = t } }

// WithDevRoot sets the dev root directory mounted into the VM (convenience — creates one rw mount).
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

// WithRecreatePromptAfter configures the VM age threshold after which the user is prompted.
// Use config.DisabledDuration to disable the prompt entirely.
func WithRecreatePromptAfter(d time.Duration) Option {
	return func(c *testConfig) { c.RecreatePromptAfter = d }
}

// WithBaseImageRebuildPromptAfter configures the base image age threshold after which the user is prompted.
// Use config.DisabledDuration to disable the prompt entirely.
func WithBaseImageRebuildPromptAfter(d time.Duration) Option {
	return func(c *testConfig) { c.BaseImageRebuildPromptAfter = d }
}

// WithMaxAgeDays is a convenience wrapper for WithRecreatePromptAfter using days.
func WithMaxAgeDays(days int) Option {
	return WithRecreatePromptAfter(time.Duration(days) * 24 * time.Hour)
}

// WithBaseImageMaxAgeDays is a convenience wrapper for WithBaseImageRebuildPromptAfter using days.
func WithBaseImageMaxAgeDays(days int) Option {
	return WithBaseImageRebuildPromptAfter(time.Duration(days) * 24 * time.Hour)
}

// WithProvider selects the AI agent provider by name (e.g. "claude", "copilot").
// Defaults to "claude".
func WithProvider(name string) Option {
	return func(c *testConfig) { c.Provider = name }
}

// WithInteractive simulates running in an interactive terminal.
// The provided answers are fed to stdin prompts in order (one per prompt).
// Without this option the CLI behaves non-interactively (all prompt code paths
// are bypassed).
func WithInteractive(answers ...string) Option {
	return func(c *testConfig) {
		c.Interactive = true
		c.StdinAnswers = answers
	}
}

// WithIntegrations appends extra integrations to the test harness alongside the
// default stub integrations. Use this to test custom user-defined integrations.
func WithIntegrations(defs ...integration.IntegrationDef) Option {
	return func(c *testConfig) {
		c.Integrations = append(c.Integrations, defs...)
	}
}

func buildTestConfig(profile, stateDir string, tc testConfig) *config.Config {
	var parsedMounts []config.Mount
	if tc.DevRoot != "" {
		parsedMounts = []config.Mount{{HostPath: tc.DevRoot, Writable: true}}
	}

	recreatePromptAfter := tc.RecreatePromptAfter
	if recreatePromptAfter == 0 {
		recreatePromptAfter = config.DisabledDuration
	}
	baseImageRebuildPromptAfter := tc.BaseImageRebuildPromptAfter
	if baseImageRebuildPromptAfter == 0 {
		baseImageRebuildPromptAfter = config.DisabledDuration
	}

	return &config.Config{
		VM: config.VMConfig{
			CPUs:                                 tc.CPUs,
			MemoryBytes:                          tc.MemoryBytes,
			DiskBytes:                            tc.DiskBytes,
			Type:                                 tc.VMType,
			RecreatePromptAfterDuration:          recreatePromptAfter,
			BaseImageRebuildPromptAfterDuration:  baseImageRebuildPromptAfter,
			ParsedMounts:                         parsedMounts,
			ColimaProfile:                        profile,
		},
		MCP: config.MCPConfig{
			Enable:     true,
			Port:       19999, // unused — MCP is stubbed
			DataDir:    filepath.Join(stateDir, "mcpjungle-data"),
			ImageTag:   "latest",
			ServerMode: "development",
		},
		Idle: config.IdleConfig{
			StopTimeout:   tc.IdleTimeout,
			DeleteTimeout: tc.DeleteTimeout,
		},
		Agents: config.AgentsConfig{
			Enabled: tc.Provider,
		},
		Plugins: config.PluginsConfig{
			Enabled: tc.Plugins,
		},
		StateDir: stateDir,
	}
}

