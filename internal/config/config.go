package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/integration"
	"github.com/sisimomo/aivm/internal/plugin"
)

//go:embed defaults.yaml
var defaultsYAML []byte

type Config struct {
	VM VMConfig `mapstructure:"vm"`
	// ComposeFile is the path to a docker compose file whose services are
	// started and stopped alongside the VM. The raw value from YAML is resolved
	// to an absolute path relative to the config file directory during loading.
	ComposeFile  string                       `mapstructure:"compose_file"`
	T3Code       T3CodeConfig                 `mapstructure:"t3code"`
	Idle         IdleConfig                   `mapstructure:"idle"`
	Agents       AgentsConfig                 `mapstructure:"agents"`
	Plugins      PluginsConfig                `mapstructure:"plugins"`
	Integrations []integration.IntegrationDef `mapstructure:"integrations"`
	Debug        bool                         `mapstructure:"debug"`

	StateDir string `mapstructure:"-"`
}

// Mount represents a single host directory mounted into the VM.
type Mount struct {
	HostPath string
	Writable bool
}

// VMConfig holds VM configuration. String fields use human-readable units
// (e.g. "8GB", "7d") and are validated and parsed into the Parsed* fields
// during config loading via validateAndParse.
type VMConfig struct {
	CPUs   int               `mapstructure:"cpus"`
	Memory string            `mapstructure:"memory"` // "8GB", "512MB", "1TB"
	Disk   string            `mapstructure:"disk"`   // "60GB"
	Type   string            `mapstructure:"type"`   // "vz", "qemu", or "" for auto-detect
	Mounts []string          `mapstructure:"mounts"` // ["~/dev:rw", "~/.ssh:ro"]
	Env    map[string]string `mapstructure:"env"`    // arbitrary env vars injected into every VM session
	Name   string            `mapstructure:"name"`   // VM identity (Colima profile name / Docker container name)

	// Backend selects the VM runtime. Supported values: "colima" (default), "docker".
	Backend string `mapstructure:"backend"`

	// DockerImage is the Docker image used when backend is "docker".
	DockerImage string `mapstructure:"docker_image"`

	// RecreatePromptAfter is the staleness threshold after which the user is
	// prompted to recreate the VM. Format: "7d", "12h", or "-1" to disable.
	RecreatePromptAfter string `mapstructure:"recreate_prompt_after"`

	// BaseImageRebuildPromptAfter is the staleness threshold after which the
	// user is prompted to rebuild the base image. Format: "7d", "12h", "-1".
	BaseImageRebuildPromptAfter string `mapstructure:"base_image_rebuild_prompt_after"`

	// Parsed fields — populated by validateAndParse, never read from YAML.
	MemoryBytes                         int64         `mapstructure:"-"`
	DiskBytes                           int64         `mapstructure:"-"`
	RecreatePromptAfterDuration         time.Duration `mapstructure:"-"` // DisabledDuration = prompt off
	BaseImageRebuildPromptAfterDuration time.Duration `mapstructure:"-"` // DisabledDuration = prompt off
	ParsedMounts                        []Mount       `mapstructure:"-"`
}

// Profile returns the VM identity used as the Colima profile name or Docker
// container name. Falls back to "aivm" if vm.name is not set.
func (vm *VMConfig) Profile() string {
	if vm.Name != "" {
		return vm.Name
	}
	return "aivm"
}

// T3CodeConfig holds configuration for the optional T3 Code web GUI integration.
// When enabled, `aivm launch` starts t3 serve inside the VM and port-forwards it
// to the host instead of launching an agent CLI session. Idle monitoring is
// automatically disabled when T3 Code is enabled.
type T3CodeConfig struct {
	Enable bool `mapstructure:"enable"`
	Port   int  `mapstructure:"port"`
}

type IdleConfig struct {
	StopTimeout   time.Duration `mapstructure:"stop_timeout"`
	DeleteTimeout time.Duration `mapstructure:"delete_timeout"`
	PollInterval  time.Duration `mapstructure:"poll_interval"`
}

// AgentsConfig is the top-level agents registry. It is independent of plugins.
type AgentsConfig struct {
	// Default is the name of the agent launched when aivm is run without --agent.
	Default string `mapstructure:"default"`
	// Define holds per-agent definition entries keyed by agent name.
	// Set enable: true on any agent to have it bootstrapped in the VM.
	// Other fields (launch_command, setup, etc.) may also be overridden here.
	Define map[string]agent.Def `mapstructure:"define"`
}

type PluginsConfig struct {
	Enabled []string                    `mapstructure:"enabled"`
	Config  map[string]map[string]any   `mapstructure:"config"`
	Define  map[string]plugin.PluginDef `mapstructure:"define"`
}

// Defaults holds build-time-injectable values so that a dev build can use a
// separate state directory without conflicting with the production install.
type Defaults struct {
	// StateDir is the raw (unexpanded) path used as the home state directory,
	// e.g. "~/.aivm" for prod or "~/.aivm-dev" for dev.
	StateDir string
}

// ActiveAgents returns the names of all agents with enable: true in agents.define.
func (c *Config) ActiveAgents() []string {
	var names []string
	for name, def := range c.Agents.Define {
		if def.Enable {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (c *Config) DefaultAgent() string {
	return c.Agents.Default
}

// ResolvedEnv returns vm.env with all ${HOST_VAR} and $HOST_VAR references
// expanded from the host environment. Use this whenever env values are
// applied to the VM or hashed for change detection.
func (vm *VMConfig) ResolvedEnv() map[string]string {
	if len(vm.Env) == 0 {
		return nil
	}
	resolved := make(map[string]string, len(vm.Env))
	for k, v := range vm.Env {
		resolved[k] = os.ExpandEnv(v)
	}
	return resolved
}

// Load reads aivm.yaml from the given path (or searches standard locations).
// d provides build-time defaults (StateDir) so dev and prod builds stay isolated.
func Load(cfgPath string, d Defaults) (*Config, error) {
	v := viper.New()

	if err := setDefaultsFromYAML(v, defaultsYAML); err != nil {
		return nil, fmt.Errorf("loading config defaults: %w", err)
	}

	v.SetEnvPrefix("AIVM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	home, _ := os.UserHomeDir()
	stateDir := expandPath(d.StateDir, home)
	if override := os.Getenv("AIVM_STATE_DIR"); override != "" {
		stateDir = expandPath(override, home)
	}

	if cfgPath != "" {
		v.SetConfigFile(expandHome(cfgPath))
	} else {
		v.SetConfigName("aivm")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if repoRoot := os.Getenv("AIVM_REPO_ROOT"); repoRoot != "" {
			v.AddConfigPath(repoRoot)
		}
		v.AddConfigPath(stateDir)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Viper normalises all map keys to lowercase, which corrupts vm.env and
	// plugins.config keys (e.g. AIVM_BOOT_VAR → aivm_boot_var). Re-read the
	// raw YAML to recover original case for those fields.
	if err := preserveRawYAMLFields(v, &cfg); err != nil {
		return nil, err
	}

	cfg.StateDir = stateDir

	if err := validateAndParse(&cfg, home); err != nil {
		return nil, err
	}

	// Resolve compose_file path relative to the config file directory.
	if cfg.ComposeFile != "" {
		// Expand tilde (~/) to user's home directory
		if strings.HasPrefix(cfg.ComposeFile, "~/") {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("expanding ~ in compose_file: %w", err)
			}
			cfg.ComposeFile = filepath.Join(homeDir, cfg.ComposeFile[2:])
		}

		if !filepath.IsAbs(cfg.ComposeFile) {
			if cfgFile := v.ConfigFileUsed(); cfgFile != "" {
				cfg.ComposeFile = filepath.Join(filepath.Dir(cfgFile), cfg.ComposeFile)
			}
		}
		absPath, err := filepath.Abs(cfg.ComposeFile)
		if err != nil {
			return nil, fmt.Errorf("resolving compose_file to absolute path: %w", err)
		}
		cfg.ComposeFile = absPath
	}

	return &cfg, nil
}

// validateAndParse validates raw config values and populates all Parsed* fields
// on VMConfig. Hard errors are returned immediately — no silent coercion.
func validateAndParse(cfg *Config, home string) error {
	vm := &cfg.VM

	// --- memory ---
	memBytes, err := ParseResourceBytes(vm.Memory)
	if err != nil {
		return fmt.Errorf("vm.memory: %w", err)
	}
	vm.MemoryBytes = memBytes

	// --- disk ---
	diskBytes, err := ParseResourceBytes(vm.Disk)
	if err != nil {
		return fmt.Errorf("vm.disk: %w", err)
	}
	vm.DiskBytes = diskBytes

	// --- vm type ---
	switch vm.Type {
	case "", "vz", "qemu":
		// valid (empty = auto-detect at runtime)
	default:
		return fmt.Errorf("vm.type: unsupported value %q — use \"vz\", \"qemu\", or omit for auto-detection", vm.Type)
	}

	// --- backend ---
	switch vm.Backend {
	case "", "colima", "docker":
		// valid
	default:
		return fmt.Errorf("vm.backend: unsupported value %q — use \"colima\" or \"docker\"", vm.Backend)
	}

	// --- vm name (required for colima backend) ---
	if (vm.Backend == "" || vm.Backend == "colima") && vm.Name == "" {
		return fmt.Errorf("vm.name: must not be empty when using the colima backend")
	}

	// --- docker image (required for docker backend) ---
	if vm.Backend == "docker" && vm.DockerImage == "" {
		return fmt.Errorf("vm.docker_image: must not be empty when using the docker backend")
	}

	// --- recreate_prompt_after ---
	rpa, err := ParsePromptDuration(vm.RecreatePromptAfter)
	if err != nil {
		return fmt.Errorf("vm.recreate_prompt_after: %w", err)
	}
	vm.RecreatePromptAfterDuration = rpa

	// --- base_image_rebuild_prompt_after ---
	bipa, err := ParsePromptDuration(vm.BaseImageRebuildPromptAfter)
	if err != nil {
		return fmt.Errorf("vm.base_image_rebuild_prompt_after: %w", err)
	}
	vm.BaseImageRebuildPromptAfterDuration = bipa

	// --- env ---
	for name := range vm.Env {
		if err := ValidateEnvVarName(name); err != nil {
			return fmt.Errorf("vm.env: %w", err)
		}
	}

	// --- mounts ---
	parsed := make([]Mount, 0, len(vm.Mounts))
	for _, spec := range vm.Mounts {
		m, err := ParseMount(spec, home)
		if err != nil {
			return fmt.Errorf("vm.mounts: %w", err)
		}
		parsed = append(parsed, m)
	}
	vm.ParsedMounts = parsed

	return nil
}

func expandHome(path string) string {
	home, _ := os.UserHomeDir()
	return expandPath(path, home)
}

func expandPath(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// setDefaultsFromYAML parses the given YAML bytes and registers each leaf value
// as a viper default using dot-separated keys (e.g. "vm.cpus").
func setDefaultsFromYAML(v *viper.Viper, data []byte) error {
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return err
	}
	setDefaultsFromMap(v, m, "")
	return nil
}

func setDefaultsFromMap(v *viper.Viper, m map[string]any, prefix string) {
	for k, val := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if nested, ok := val.(map[string]any); ok {
			setDefaultsFromMap(v, nested, key)
		} else {
			v.SetDefault(key, val)
		}
	}
}

// preserveRawYAMLFields re-reads the raw config file (if any) and overlays
// selected fields whose keys must not be lowercased. This works around
// Viper's internal key-normalisation, which lowercases all map keys before
// unmarshaling (e.g. AIVM_BOOT_VAR → aivm_boot_var, VOYAGE_API_KEY →
// voyage_api_key inside plugins.config).
func preserveRawYAMLFields(v *viper.Viper, cfg *Config) error {
	cfgFile := v.ConfigFileUsed()
	if cfgFile == "" {
		return nil
	}
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		// Non-fatal: Viper already read the file; this is just a case-fix pass.
		return nil
	}
	var raw struct {
		VM struct {
			Env map[string]string `yaml:"env"`
		} `yaml:"vm"`
		Plugins struct {
			Config map[string]map[string]any `yaml:"config"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("re-reading raw config fields: %w", err)
	}
	if len(raw.VM.Env) > 0 {
		cfg.VM.Env = raw.VM.Env
	}
	if len(raw.Plugins.Config) > 0 {
		cfg.Plugins.Config = raw.Plugins.Config
	}
	return nil
}
