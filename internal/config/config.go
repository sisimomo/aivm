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
	RepoRoot string `mapstructure:"-"`
}

type VMConfig struct {
	CPUs       int    `mapstructure:"cpus"`
	MemoryGiB  int    `mapstructure:"memory"`
	DiskGiB    int    `mapstructure:"disk"`
	Type       string `mapstructure:"type"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	DevRoot    string `mapstructure:"dev_root"`
	Profile    string `mapstructure:"profile"`
}

type MCPConfig struct {
	Port       int    `mapstructure:"port"`
	DataDir    string `mapstructure:"data_dir"`
	ImageTag   string `mapstructure:"image_tag"`
	ServerMode string `mapstructure:"server_mode"`
}

type IdleConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
}

type AuthConfig struct {
	ClaudeToken string `mapstructure:"claude_token"`
}

type PluginsConfig struct {
	Enabled []string                   `mapstructure:"enabled"`
	Config  map[string]map[string]any  `mapstructure:"config"`
	Define  map[string]plugin.PluginDef `mapstructure:"define"`
}

// Load reads aivm.yaml from the given path (or searches standard locations).
func Load(cfgPath string) (*Config, error) {
	v := viper.New()

	v.SetDefault("vm.cpus", 4)
	v.SetDefault("vm.memory", 8)
	v.SetDefault("vm.disk", 60)
	v.SetDefault("vm.type", "vz")
	v.SetDefault("vm.max_age_days", 7)
	v.SetDefault("vm.dev_root", "~/dev")
	v.SetDefault("vm.profile", "aivm")
	v.SetDefault("mcp.port", 8080)
	v.SetDefault("mcp.data_dir", "~/.aivm/mcpjungle-data")
	v.SetDefault("mcp.image_tag", "latest-stdio")
	v.SetDefault("mcp.server_mode", "development")
	v.SetDefault("idle.timeout", "5m")
	v.SetDefault("plugins.enabled", []string{"system", "java", "maven", "nodejs", "python", "rtk", "claude"})

	v.SetEnvPrefix("AIVM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgPath != "" {
		v.SetConfigFile(expandHome(cfgPath))
	} else {
		v.SetConfigName("aivm")
		v.SetConfigType("yaml")
		home, _ := os.UserHomeDir()
		v.AddConfigPath(".")
		if repoRoot := os.Getenv("AIVM_REPO_ROOT"); repoRoot != "" {
			v.AddConfigPath(repoRoot)
		}
		v.AddConfigPath(filepath.Join(home, ".aivm"))
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

	home, _ := os.UserHomeDir()
	cfg.VM.DevRoot = expandPath(cfg.VM.DevRoot, home)
	cfg.MCP.DataDir = expandPath(cfg.MCP.DataDir, home)
	cfg.StateDir = filepath.Join(home, ".aivm")

	if v.ConfigFileUsed() != "" {
		cfg.RepoRoot = filepath.Dir(v.ConfigFileUsed())
	} else {
		exe, _ := os.Executable()
		cfg.RepoRoot = filepath.Dir(filepath.Dir(exe))
	}

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
