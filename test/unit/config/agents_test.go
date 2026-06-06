package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
)

// --- ActiveAgents ---

func TestActiveAgents_EmptyConfig(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	got := cfg.ActiveAgents()
	if len(got) != 0 {
		t.Errorf("ActiveAgents() with empty config: got %v, want empty", got)
	}
}

func TestActiveAgents_NoneEnabledInDefine(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Define: map[string]config.AgentDefine{
				"claude": {Enable: false},
			},
		},
	}
	got := cfg.ActiveAgents()
	if len(got) != 0 {
		t.Errorf("ActiveAgents() with none enabled: got %v, want empty", got)
	}
}

func TestActiveAgents_SingleEnabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Define: map[string]config.AgentDefine{
				"claude": {Enable: true},
			},
		},
	}
	got := cfg.ActiveAgents()
	if len(got) != 1 || got[0] != "claude" {
		t.Errorf("ActiveAgents() = %v, want [claude]", got)
	}
}

func TestActiveAgents_MultipleEnabled_Sorted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Define: map[string]config.AgentDefine{
				"copilot":  {Enable: true},
				"claude":   {Enable: true},
				"opencode": {Enable: false},
			},
		},
	}
	got := cfg.ActiveAgents()
	if len(got) != 2 {
		t.Fatalf("ActiveAgents() = %v, want 2 entries", got)
	}
	if got[0] != "claude" || got[1] != "copilot" {
		t.Errorf("ActiveAgents() = %v, want [claude copilot] (sorted)", got)
	}
}

func TestActiveAgents_AllFourEnabled_Sorted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Define: map[string]config.AgentDefine{
				"opencode": {Enable: true},
				"copilot":  {Enable: true},
				"cursor":   {Enable: true},
				"claude":   {Enable: true},
			},
		},
	}
	got := cfg.ActiveAgents()
	if len(got) != 4 {
		t.Fatalf("ActiveAgents() = %v, want 4 entries", got)
	}
	want := []string{"claude", "copilot", "cursor", "opencode"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("ActiveAgents()[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestLoad_RejectsUnknownAgentDefineField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "aivm.yaml")
	const content = `
agents:
  define:
    claude:
      enable: true
      launch_command: "nope"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path, config.Defaults{StateDir: dir})
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %q, want unknown field", err.Error())
	}
	if !strings.Contains(err.Error(), "launch_command") {
		t.Fatalf("error = %q, want launch_command mentioned", err.Error())
	}
}

func TestAgentDefine_ApplyTo_MergesNonZeroFields(t *testing.T) {
	t.Parallel()
	base := agent.Def{CLICommand: "claude", Description: "built-in"}
	override := config.AgentDefine{CLICommand: "claude-cli", Enable: true}
	got := override.ApplyTo(base)
	if got.CLICommand != "claude-cli" {
		t.Errorf("CLICommand = %q, want claude-cli", got.CLICommand)
	}
	if !got.Enable {
		t.Error("Enable = false, want true")
	}
	if got.Description != "built-in" {
		t.Errorf("Description = %q, want built-in", got.Description)
	}
}

// --- DefaultAgent ---

func TestDefaultAgent_ReturnsDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{Default: "claude"},
	}
	if got := cfg.DefaultAgent(); got != "claude" {
		t.Errorf("DefaultAgent() = %q, want %q", got, "claude")
	}
}

func TestDefaultAgent_EmptyDefault_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	if got := cfg.DefaultAgent(); got != "" {
		t.Errorf("DefaultAgent() = %q, want empty string", got)
	}
}
