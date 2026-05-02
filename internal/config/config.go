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

	"aivm/internal/plugin"
)

//go:embed defaults.yaml
var defaultsYAML []byte

type Config struct {
	VM      VMConfig      `mapstructure:"vm"`
	MCP     MCPConfig     `mapstructure:"mcp"`
	Idle    IdleConfig    `mapstructure:"idle"`
	Agent   AgentConfig   `mapstructure:"agent"`
	Plugins PluginsConfig `mapstructure:"plugins"`
	Debug   bool          `mapstructure:"debug"`

	StateDir string `mapstructure:"-"`
}

type VMConfig struct {
	CPUs                int    `mapstructure:"cpus"`
	MemoryGiB           int    `mapstructure:"memory"`
	DiskGiB             int    `mapstructure:"disk"`
	Type                string `mapstructure:"type"`
	MaxAgeDays          int    `mapstructure:"max_age_days"`
	BaseImageMaxAgeDays int    `mapstructure:"base_image_max_age_days"`
	DevRoot             string `mapstructure:"dev_root"`
	Profile             string `mapstructure:"profile"`
}

type MCPConfig struct {
	Port       int    `mapstructure:"port"`
	DataDir    string `mapstructure:"data_dir"`
	ImageTag   string `mapstructure:"image_tag"`
	ServerMode string `mapstructure:"server_mode"`
}

type IdleConfig struct {
	Timeout       time.Duration `mapstructure:"timeout"`
	DeleteTimeout time.Duration `mapstructure:"delete_timeout"`
}

// AgentConfig configures which AI agent provider is active and holds
// provider-specific settings.
type AgentConfig struct {
	// Provider is the name of the active agent provider (e.g. "claude", "copilot").
	Provider  string                    `mapstructure:"provider"`
	// Providers holds per-provider configuration keyed by provider name.
	Providers map[string]map[string]any `mapstructure:"providers"`
}

type PluginsConfig struct {
	Enabled []string                    `mapstructure:"enabled"`
	Config  map[string]map[string]any   `mapstructure:"config"`
	Define  map[string]plugin.PluginDef `mapstructure:"define"`
}

// Defaults holds build-time-injectable values so that a dev build can use a
// separate state directory, Colima profile, and MCP port without conflicting
// with the production install.
type Defaults struct {
	// StateDir is the raw (unexpanded) path used as the home state directory,
	// e.g. "~/.aivm" for prod or "~/.aivm-dev" for dev.
	StateDir string
	// VMProfile is the default Colima profile name, e.g. "aivm" or "aivm-dev".
	VMProfile string
	// MCPPort is the default port for the MCP/mcpjungle service.
	MCPPort int
}

// Load reads aivm.yaml from the given path (or searches standard locations).
// d provides build-time defaults so dev and prod builds stay isolated.
func Load(cfgPath string, d Defaults) (*Config, error) {
	v := viper.New()

	if err := setDefaultsFromYAML(v, defaultsYAML); err != nil {
		return nil, fmt.Errorf("loading config defaults: %w", err)
	}
	v.SetDefault("vm.profile", d.VMProfile)
	v.SetDefault("mcp.port", d.MCPPort)
	v.SetDefault("mcp.data_dir", d.StateDir+"/mcpjungle-data")

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

	cfg.VM.DevRoot = expandPath(cfg.VM.DevRoot, home)
	cfg.MCP.DataDir = expandPath(cfg.MCP.DataDir, home)
	cfg.StateDir = stateDir

	return &cfg, nil
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
