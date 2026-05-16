package framework

import (
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FreePort asks the OS for a free TCP port and returns it. It panics if no
// port can be allocated. Use this in tests that need a specific port to pass
// to WithT3Code so that Docker can bind it on the host without collisions.
func FreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("framework.FreePort: %v", err))
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// testConfig holds configuration for a test Harness. It uses small defaults
// suitable for tests (minimal VM resources, short idle timeouts).
type testConfig struct {
	CPUs    int
	Memory  string // "2GB"
	Disk    string // "10GB"
	DevRoot string // convenience: creates a single rw mount

	IdleTimeout   time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration

	// RecreatePromptAfter in ParsePromptDuration format: "-1" or "Nd".
	RecreatePromptAfter string
	// BaseImageRebuildPromptAfter in ParsePromptDuration format: "-1" or "Nd".
	BaseImageRebuildPromptAfter string

	// Provider selects the AI agent provider name (default "claude").
	Provider string

	// LaunchCommand, when non-empty, overrides the launch_command for the
	// active provider. Tests that need a long-lived agent process (e.g. session
	// idle tests) set this to "sleep 30" to hold a session lock open without
	// requiring interactive TUI auth.
	LaunchCommand string

	// Interactive, when true, sets AIVM_FORCE_INTERACTIVE=1 in subprocess env.
	Interactive bool
	// StdinAnswers is fed to the subprocess stdin, one answer per prompt.
	StdinAnswers []string

	// T3CodeEnabled, when true, sets t3code.enable: true in the YAML config.
	T3CodeEnabled bool
	// T3CodePort sets t3code.port in the YAML. 0 = auto-assign by Docker.
	T3CodePort int

	// Plugins is the list of plugins.enabled entries.
	Plugins []string
	// VMEnv sets vm.env in the YAML.
	VMEnv map[string]string
	// ComposeContent, when non-empty, is written to <stateDir>/docker-compose.yml
	// and referenced as compose_file in aivm.yaml.
	ComposeContent string
}

func defaultTestConfig() testConfig {
	return testConfig{
		CPUs:                        1,
		Memory:                      "2GB",
		Disk:                        "10GB",
		IdleTimeout:                 10 * time.Second,
		DeleteTimeout:               10 * time.Second,
		PollInterval:                1 * time.Second,
		RecreatePromptAfter:         "-1",
		BaseImageRebuildPromptAfter: "-1",
		Provider:                    "claude",
		Plugins:                     []string{},
	}
}

// Option is a functional option for configuring a test Harness.
type Option func(*testConfig)

// WithCPUs sets the number of vCPUs for the test VM.
func WithCPUs(n int) Option { return func(c *testConfig) { c.CPUs = n } }

// WithMemoryGiB sets the RAM (GiB) for the test VM.
func WithMemoryGiB(n int) Option { return func(c *testConfig) { c.Memory = fmt.Sprintf("%dGB", n) } }

// WithDiskGiB sets the disk size (GiB) for the test VM.
func WithDiskGiB(n int) Option { return func(c *testConfig) { c.Disk = fmt.Sprintf("%dGB", n) } }

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

// WithMaxAgeDays configures the VM age threshold (in days) after which the user
// is prompted to recreate the VM.
func WithMaxAgeDays(days int) Option {
	return func(c *testConfig) {
		if days == -1 {
			c.RecreatePromptAfter = "-1"
		} else {
			c.RecreatePromptAfter = fmt.Sprintf("%dd", days)
		}
	}
}

// WithBaseImageMaxAgeDays configures the base image age threshold (in days)
// after which the user is prompted to rebuild the base image.
func WithBaseImageMaxAgeDays(days int) Option {
	return func(c *testConfig) {
		if days == -1 {
			c.BaseImageRebuildPromptAfter = "-1"
		} else {
			c.BaseImageRebuildPromptAfter = fmt.Sprintf("%dd", days)
		}
	}
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

// WithT3Code enables T3 Code mode for the test harness. Pass a specific port
// from FreePort() for T3CodePortAccessible assertions, or 0 for auto-assign.
func WithT3Code(port int) Option {
	return func(c *testConfig) {
		c.T3CodeEnabled = true
		if port > 0 {
			c.T3CodePort = port
		}
	}
}

// WithVMEnv sets vm.env for the test Harness. Env vars are written to the VM's
// /etc/profile.d/aivm-user-env.sh on every bootstrap.
func WithVMEnv(env map[string]string) Option {
	return func(c *testConfig) { c.VMEnv = env }
}

// WithComposeContent sets the docker-compose.yml content for the test Harness.
// The framework writes it to <stateDir>/docker-compose.yml and references it
// as compose_file in aivm.yaml. Use standard docker compose YAML.
func WithComposeContent(content string) Option {
	return func(c *testConfig) { c.ComposeContent = content }
}

// WithLaunchCommand overrides the launch_command for the active provider in
// the generated aivm.yaml. Use this when a test needs a long-lived agent
// process (e.g. "sleep 30" for session idle tests) rather than the default
// version-check override. The real agent binary is still installed during
// bootstrap — only the launch command is overridden.
func WithLaunchCommand(cmd string) Option {
	return func(c *testConfig) { c.LaunchCommand = cmd }
}

// effectivePlugins returns the plugins.enabled list, adding "t3code" when
// T3Code is enabled (mirroring CompositionEngine's auto-injection).
func effectivePlugins(tc testConfig) []string {
	plugins := append([]string{}, tc.Plugins...)
	if tc.T3CodeEnabled {
		for _, p := range plugins {
			if p == "t3code" {
				return plugins
			}
		}
		plugins = append(plugins, "t3code")
	}
	return plugins
}

// agentLaunchCommand returns the launch_command to use in the test config for
// the given agent name. By default it runs the real binary version check and
// appends a line to the agent-launched marker file so tests can count launches
// without requiring interactive TUI auth. WithLaunchCommand overrides this.
//
// The wrapper uses `bash -lc` because the generic provider runs `exec %s`,
// which replaces the shell process. Any compound command after `exec` (e.g.
// `exec claude --version && echo 1 >> marker`) would never reach the `echo`
// because exec discards the remaining shell commands. Wrapping in a login
// subshell ensures the marker write happens first, then the binary is exec'd
// inside the subshell where it CAN run to completion.
func agentLaunchCommand(name string, override string) string {
	if override != "" {
		return override
	}
	// Write to the marker file first (inside a login subshell so PATH is set),
	// then exec the real binary. The login shell sources /etc/profile.d/ so
	// mise shims and agent path entries are available.
	return "bash -lc 'echo 1 >> /tmp/.aivm_agent_launched; exec " + name + " --version'"
}

// buildTestYAML generates the aivm.yaml content for the test harness subprocess.
// Real agent and plugin install scripts are used — no stubs. Agent launch
// commands are overridden to version-check invocations so tests do not require
// interactive TUI sessions or auth tokens, while still proving the binaries are
// installed and callable.
func buildTestYAML(profile, stateDir string, tc testConfig) string {
	var sb strings.Builder

	// ── vm ────────────────────────────────────────────────────────────────
	fmt.Fprintf(&sb, "vm:\n")
	fmt.Fprintf(&sb, "  cpus: %d\n", tc.CPUs)
	fmt.Fprintf(&sb, "  memory: %q\n", tc.Memory)
	fmt.Fprintf(&sb, "  disk: %q\n", tc.Disk)
	fmt.Fprintf(&sb, "  backend: docker\n")
	fmt.Fprintf(&sb, "  docker_image: %q\n", TestImageName)
	fmt.Fprintf(&sb, "  name: %q\n", profile)
	fmt.Fprintf(&sb, "  recreate_prompt_after: %q\n", tc.RecreatePromptAfter)
	fmt.Fprintf(&sb, "  base_image_rebuild_prompt_after: %q\n", tc.BaseImageRebuildPromptAfter)
	if tc.DevRoot != "" {
		fmt.Fprintf(&sb, "  mounts:\n")
		fmt.Fprintf(&sb, "    - %q\n", tc.DevRoot+":rw")
	}
	if len(tc.VMEnv) > 0 {
		keys := make([]string, 0, len(tc.VMEnv))
		for k := range tc.VMEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&sb, "  env:\n")
		for _, k := range keys {
			fmt.Fprintf(&sb, "    %s: %q\n", k, tc.VMEnv[k])
		}
	}

	// ── t3code ────────────────────────────────────────────────────────────
	fmt.Fprintf(&sb, "t3code:\n")
	fmt.Fprintf(&sb, "  enable: %v\n", tc.T3CodeEnabled)
	fmt.Fprintf(&sb, "  port: %d\n", tc.T3CodePort)

	// ── idle ──────────────────────────────────────────────────────────────
	fmt.Fprintf(&sb, "idle:\n")
	fmt.Fprintf(&sb, "  stop_timeout: %q\n", tc.IdleTimeout.String())
	fmt.Fprintf(&sb, "  delete_timeout: %q\n", tc.DeleteTimeout.String())
	fmt.Fprintf(&sb, "  poll_interval: %q\n", tc.PollInterval.String())

	// ── agents ────────────────────────────────────────────────────────────
	// Only launch_command is overridden — setup and skip_if come from
	// defaults.yaml (real curl-based install scripts). This proves the real
	// binary is installed and callable without requiring interactive TUI auth.
	fmt.Fprintf(&sb, "agents:\n")
	fmt.Fprintf(&sb, "  enabled: %q\n", tc.Provider)
	fmt.Fprintf(&sb, "  define:\n")
	for _, name := range []string{"claude", "copilot", "opencode"} {
		launchCmd := agentLaunchCommand(name, "")
		if name == tc.Provider && tc.LaunchCommand != "" {
			launchCmd = tc.LaunchCommand
		}
		fmt.Fprintf(&sb, "    %s:\n", name)
		fmt.Fprintf(&sb, "      launch_command: %q\n", launchCmd)
	}

	// ── plugins ───────────────────────────────────────────────────────────
	// No stub definitions — real plugin install scripts run during bootstrap.
	plugins := effectivePlugins(tc)
	fmt.Fprintf(&sb, "plugins:\n")
	if len(plugins) == 0 {
		fmt.Fprintf(&sb, "  enabled: []\n")
	} else {
		fmt.Fprintf(&sb, "  enabled:\n")
		for _, p := range plugins {
			fmt.Fprintf(&sb, "    - %q\n", p)
		}
	}

	// ── compose_file ──────────────────────────────────────────────────────
	if tc.ComposeContent != "" {
		composePath := filepath.Join(stateDir, "docker-compose.yml")
		fmt.Fprintf(&sb, "compose_file: %q\n", composePath)
	}

	return sb.String()
}
