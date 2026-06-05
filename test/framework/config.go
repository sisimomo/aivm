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

	// Provider selects the AI agent provider name (default "claude").
	Provider string

	// LaunchCommand, when non-empty, overrides cli_command and launch_args for the
	// active provider. Use "sleep 30" for session idle tests. Space-separated
	// values split into cli_command and launch_args (e.g. "sleep 30").
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
	// ExtraEnabledAgents contains additional agent names to mark as enabled
	// alongside Provider. All named agents are installed during bootstrap.
	ExtraEnabledAgents []string
	// PreserveCLIAgents lists enabled agents that keep built-in cli_command and
	// launch_args (no non-interactive test wrapper). Use for aivm agent -- passthrough.
	PreserveCLIAgents []string
	// VMEnv sets vm.env in the YAML.
	VMEnv map[string]string
	// SessionEnv sets vm.session_env in the YAML.
	SessionEnv map[string]string
	// ComposeContent, when non-empty, is written to <stateDir>/docker-compose.yml
	// and referenced as compose_file in aivm.yaml.
	ComposeContent string
}

func defaultTestConfig() testConfig {
	return testConfig{
		CPUs:                1,
		Memory:              "2GB",
		Disk:                "10GB",
		IdleTimeout:         10 * time.Second,
		DeleteTimeout:       10 * time.Second,
		PollInterval:        1 * time.Second,
		RecreatePromptAfter: "-1",
		Provider:            "claude",
		Plugins:             []string{},
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

// WithProvider selects the AI agent provider by name (e.g. "claude", "copilot").
// Defaults to "claude".
func WithProvider(name string) Option {
	return func(c *testConfig) { c.Provider = name }
}

// WithExtraAgents marks additional agents as enabled in the test config.
// Together with Provider they form the complete set of agents installed during
// bootstrap. Use WithProvider to set the default agent (the one launched when
// no --agent flag is given).
func WithExtraAgents(names ...string) Option {
	return func(c *testConfig) {
		c.ExtraEnabledAgents = append(c.ExtraEnabledAgents, names...)
	}
}

// WithPreserveAgentCLI keeps named enabled agents on their built-in cli_command
// instead of the non-interactive test launch wrapper. Use when a test invokes
// aivm agent -- and must hit the real agent binary.
func WithPreserveAgentCLI(names ...string) Option {
	return func(c *testConfig) {
		c.PreserveCLIAgents = append(c.PreserveCLIAgents, names...)
	}
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

// WithSessionEnv sets vm.session_env for the test Harness. Values are resolved
// from the host environment of each aivm subprocess (e.g. aivm ssh).
func WithSessionEnv(env map[string]string) Option {
	return func(c *testConfig) { c.SessionEnv = env }
}

// WithComposeContent sets the docker-compose.yml content for the test Harness.
// The framework writes it to <stateDir>/docker-compose.yml and references it
// as compose_file in aivm.yaml. Use standard docker compose YAML.
func WithComposeContent(content string) Option {
	return func(c *testConfig) { c.ComposeContent = content }
}

// WithLaunchCommand overrides cli_command and launch_args for the active provider
// in the generated aivm.yaml. Use "sleep 30" for session idle tests.
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

// agentLaunchFields returns optional cli_command override and launch_args for tests.
// By default only launch_args is set (bash -lc wrapper) so cli_command stays on the
// built-in binary — aivm agent -- can invoke the real CLI. WithLaunchCommand
// overrides the active provider (e.g. "sleep 30" sets cli_command sleep, launch_args 30).
func agentLaunchFields(name, override string) (cliCommand, launchArgs string) {
	if override != "" {
		parts := strings.SplitN(strings.TrimSpace(override), " ", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return override, ""
	}
	binName := name
	if name == "cursor" {
		binName = "agent"
	}
	return "bash", fmt.Sprintf("-lc 'echo 1 >> /tmp/.aivm_agent_launched; exec %s --version'", binName)
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
	if len(tc.SessionEnv) > 0 {
		keys := make([]string, 0, len(tc.SessionEnv))
		for k := range tc.SessionEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&sb, "  session_env:\n")
		for _, k := range keys {
			fmt.Fprintf(&sb, "    %s: %q\n", k, tc.SessionEnv[k])
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
	// cli_command and launch_args are overridden for tests — setup and skip_if
	// come from defaults.yaml (real curl-based install scripts).
	// Provider and any ExtraEnabledAgents are marked enable: true so all
	// configured agents are bootstrapped in the VM.
	extraEnabled := make(map[string]bool, len(tc.ExtraEnabledAgents))
	for _, name := range tc.ExtraEnabledAgents {
		extraEnabled[name] = true
	}
	preserveCLI := make(map[string]bool, len(tc.PreserveCLIAgents))
	for _, name := range tc.PreserveCLIAgents {
		preserveCLI[name] = true
	}
	fmt.Fprintf(&sb, "agents:\n")
	fmt.Fprintf(&sb, "  default: %q\n", tc.Provider)
	fmt.Fprintf(&sb, "  define:\n")
	for _, name := range []string{"claude", "copilot", "cursor", "opencode"} {
		fmt.Fprintf(&sb, "    %s:\n", name)
		enabled := name == tc.Provider || extraEnabled[name]
		if enabled {
			fmt.Fprintf(&sb, "      enable: true\n")
		}
		if !enabled || preserveCLI[name] {
			continue
		}
		cliCmd, launchArgs := agentLaunchFields(name, "")
		if tc.LaunchCommand != "" && name == tc.Provider {
			cliCmd, launchArgs = agentLaunchFields(name, tc.LaunchCommand)
		}
		if cliCmd != "" {
			fmt.Fprintf(&sb, "      cli_command: %q\n", cliCmd)
		}
		if launchArgs != "" {
			fmt.Fprintf(&sb, "      launch_args: %q\n", launchArgs)
		}
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
