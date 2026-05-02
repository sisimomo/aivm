package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"aivm/internal/plugin"
)

type Config struct {
	VM      VMConfig      `mapstructure:"vm"`
	MCP     MCPConfig     `mapstructure:"mcp"`
	Idle    IdleConfig    `mapstructure:"idle"`
	Auth    AuthConfig    `mapstructure:"auth"`
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

type AuthConfig struct {
	ClaudeToken string `mapstructure:"claude_token"`
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

	v.SetDefault("vm.cpus", 4)
	v.SetDefault("vm.memory", 8)
	v.SetDefault("vm.disk", 60)
	v.SetDefault("vm.type", "vz")
	v.SetDefault("vm.max_age_days", 7)
	v.SetDefault("vm.base_image_max_age_days", 7)
	v.SetDefault("vm.dev_root", "~/dev")
	v.SetDefault("vm.profile", d.VMProfile)
	v.SetDefault("mcp.port", d.MCPPort)
	v.SetDefault("mcp.data_dir", d.StateDir+"/mcpjungle-data")
	v.SetDefault("mcp.image_tag", "latest-stdio")
	v.SetDefault("mcp.server_mode", "development")
	v.SetDefault("idle.timeout", "5m")
	v.SetDefault("idle.delete_timeout", "5m")
	v.SetDefault("plugins.enabled", []string{"system", "java", "maven", "nodejs", "python", "rtk", "claude"})

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
