package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
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
	VM           VMConfig                     `mapstructure:"vm"`
	MCP          MCPConfig                    `mapstructure:"mcp_jungle"`
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
	CPUs          int      `mapstructure:"cpus"`
	Memory        string   `mapstructure:"memory"`        // "8GB", "512MB", "1TB"
	Disk          string   `mapstructure:"disk"`          // "60GB"
	Type          string   `mapstructure:"type"`          // "vz", "qemu", or "" for auto-detect
	Mounts        []string `mapstructure:"mounts"`        // ["~/dev:rw", "~/.ssh:ro"]
	ColimaProfile string   `mapstructure:"colima_profile"`

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

type MCPConfig struct {
	Enable     bool   `mapstructure:"enable"`
	Port       int    `mapstructure:"port"`
	DataDir    string `mapstructure:"data_dir"`
	ImageTag   string `mapstructure:"image_tag"`
	ServerMode string `mapstructure:"server_mode"`
}

type IdleConfig struct {
	StopTimeout   time.Duration `mapstructure:"stop_timeout"`
	DeleteTimeout time.Duration `mapstructure:"delete_timeout"`
}

// AgentsConfig is the top-level agents registry. It is independent of plugins.
// Only one agent may be active at a time; set agents.enabled to its name.
type AgentsConfig struct {
	// Enabled is the name of the active agent (e.g. "claude" or "copilot").
	// Exactly one agent is supported at a time. To switch agents, change this
	// value — the existing VM will be updated or recreated as needed.
	Enabled string `mapstructure:"enabled"`
	// Define holds optional per-agent definition overrides keyed by agent name.
	// Use this to override an agent's install scripts or runtime defaults
	// (e.g. agents.define.copilot.defaults.launch_command).
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

// ActiveAgents returns the active agent as a single-element slice, or nil if
// no agent is configured. The slice form is used by the integration executor.
func (c *Config) ActiveAgents() []string {
	if c.Agents.Enabled != "" {
		return []string{c.Agents.Enabled}
	}
	return nil
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

	cfg.MCP.DataDir = expandPath(cfg.MCP.DataDir, home)
	cfg.StateDir = stateDir

	if err := validateAndParse(&cfg, home); err != nil {
		return nil, err
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

	// --- colima profile ---
	if vm.ColimaProfile == "" {
		return fmt.Errorf("vm.colima_profile: must not be empty")
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
