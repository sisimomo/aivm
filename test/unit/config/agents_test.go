package config_test

import (
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
			Define: map[string]agent.Def{
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
			Define: map[string]agent.Def{
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
			Define: map[string]agent.Def{
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

func TestActiveAgents_AllThreeEnabled_Sorted(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Define: map[string]agent.Def{
				"opencode": {Enable: true},
				"copilot":  {Enable: true},
				"claude":   {Enable: true},
			},
		},
	}
	got := cfg.ActiveAgents()
	if len(got) != 3 {
		t.Fatalf("ActiveAgents() = %v, want 3 entries", got)
	}
	want := []string{"claude", "copilot", "opencode"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("ActiveAgents()[%d] = %q, want %q", i, got[i], w)
		}
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
