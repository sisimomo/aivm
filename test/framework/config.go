package framework

import (
	"path/filepath"
	"strings"
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
	// MaxAgeDays sets VM.MaxAgeDays (how old a VM is before the user is asked to recreate it).
	MaxAgeDays int
	// BaseImageMaxAgeDays sets VM.BaseImageMaxAgeDays (how old a base image is before rebuild prompt).
	BaseImageMaxAgeDays int
	// Provider selects the AI agent provider name (default "claude").
	Provider string
	// Interactive, when true, sets App.IsTerminal=true so interactive code paths run.
	Interactive bool
	// StdinAnswers is fed to App.Stdin, one answer per prompt (newline-separated).
	StdinAnswers []string
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
		Provider:      "claude",
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

// WithMaxAgeDays configures the VM.MaxAgeDays threshold. When the VM is older
// than this many days, DoStart will prompt the user to recreate it.
// Set to 0 (default) to disable the age check.
func WithMaxAgeDays(days int) Option {
	return func(c *testConfig) { c.MaxAgeDays = days }
}

// WithBaseImageMaxAgeDays configures the BaseImageMaxAgeDays threshold.
// When the base image is older than this many days, DoLaunch will prompt the
// user to rebuild. Set to 0 (default) to disable.
func WithBaseImageMaxAgeDays(days int) Option {
	return func(c *testConfig) { c.BaseImageMaxAgeDays = days }
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

func buildTestConfig(profile, stateDir string, tc testConfig) *config.Config {
	return &config.Config{
		VM: config.VMConfig{
			CPUs:                tc.CPUs,
			MemoryGiB:           tc.MemoryGiB,
			DiskGiB:             tc.DiskGiB,
			Type:                tc.VMType,
			MaxAgeDays:          tc.MaxAgeDays,
			BaseImageMaxAgeDays: tc.BaseImageMaxAgeDays,
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
			Provider: tc.Provider,
		},
		Plugins: config.PluginsConfig{
			Enabled: tc.Plugins,
		},
		StateDir: stateDir,
	}
}

// stdinReader returns a strings.Reader built from the test answers joined by newlines,
// or nil when no answers are configured.
func stdinReader(tc testConfig) *strings.Reader {
	if len(tc.StdinAnswers) == 0 {
		return strings.NewReader("")
	}
	return strings.NewReader(strings.Join(tc.StdinAnswers, "\n") + "\n")
}
