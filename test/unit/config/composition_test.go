package config_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/providers/generic"
)

// composeEngine returns a CompositionEngine suitable for unit tests.
func composeEngine() *config.CompositionEngine {
	return &config.CompositionEngine{Defaults: config.Defaults{StateDir: "~/.aivm"}}
}

// testRegistry builds an agent registry containing the named built-in agents.
func testRegistry(names ...string) *agent.Registry {
	rawDefs, err := agent.LoadDefs()
	if err != nil {
		panic("agent.LoadDefs: " + err.Error())
	}
	reg := agent.NewRegistry()
	for _, name := range names {
		def := rawDefs[name]
		reg.Register(generic.NewFromDef(name, def))
	}
	return reg
}

// writeYAML writes content to a temp file and returns its path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "aivm-*.yaml")
	if err != nil {
		t.Fatalf("createTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	f.Close()
	return f.Name()
}

// asCompositionError unwraps a *config.CompositionError from err.
func asCompositionError(err error) *config.CompositionError {
	var ce *config.CompositionError
	errors.As(err, &ce)
	return ce
}

// --- error path tests ---

func TestCompose_NoAgentsSection_Error(t *testing.T) {
	t.Parallel()
	// Empty YAML has no agents section at all.
	path := writeYAML(t, "")
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if ce.Stage != "load_config" {
		t.Errorf("Stage = %q, want %q", ce.Stage, "load_config")
	}
	if !strings.Contains(ce.Reason, "no agents enabled") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "no agents enabled")
	}
}

func TestCompose_EmptyEnabledList_Error(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  enabled: []
`)
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if !strings.Contains(ce.Reason, "no agents enabled") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "no agents enabled")
	}
}

func TestCompose_ColimaBackend_Error(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
vm:
  backend: colima
  name: aivm
agents:
  enabled:
    - claude
`)
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if !strings.Contains(err.Error(), "colima") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "colima")
	}
}

func TestCompose_UnknownAgentInEnabledSet_Error(t *testing.T) {
	t.Parallel()
	// "mystery" is enabled in YAML but not registered in the agent registry.
	path := writeYAML(t, `
agents:
  enabled:
    - mystery
`)
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if !strings.Contains(ce.Reason, "unknown agent") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "unknown agent")
	}
}

func registerCustomAgents(reg *agent.Registry, defs map[string]agent.Def) {
	for name, def := range defs {
		reg.Register(generic.NewFromDef(name, def))
	}
}

func TestCompose_CustomAgentInEnabled_WithDefine_HappyPath(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  enabled:
    - my-agent
  define:
    my-agent:
      cli_command: my-agent-cli
      setup: |
        echo installed
`)
	reg := testRegistry("claude")
	result, err := composeEngine().Compose(path, reg)
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	registerCustomAgents(reg, result.CustomAgentDefs)
	prov, ok := reg.Get("my-agent")
	if !ok {
		t.Fatal("my-agent not registered after custom agent registration")
	}
	if prov.Name() != "my-agent" {
		t.Errorf("provider name = %q, want my-agent", prov.Name())
	}
	def := result.EnabledAgentDefs["my-agent"]
	if def.CLICommand != "my-agent-cli" {
		t.Errorf("EnabledAgentDefs[my-agent].CLICommand = %q, want my-agent-cli", def.CLICommand)
	}
}

func TestCompose_MultipleEnabled_NoDefault_Error(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  enabled:
    - claude
    - copilot
`)
	_, err := composeEngine().Compose(path, testRegistry("claude", "copilot"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if !strings.Contains(ce.Reason, "agents.default must be set") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "agents.default must be set")
	}
}

func TestCompose_DefaultNotInEnabledSet_Error(t *testing.T) {
	t.Parallel()
	// agents.default is "copilot" but only "claude" is enabled.
	path := writeYAML(t, `
agents:
  default: copilot
  enabled:
    - claude
`)
	_, err := composeEngine().Compose(path, testRegistry("claude", "copilot"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if !strings.Contains(ce.Reason, "is not in agents.enabled") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "is not in agents.enabled")
	}
}

func TestCompose_InvalidVMSessionEnvName_Error(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  enabled:
    - claude
vm:
  session_env:
    BAD-NAME: "${HOST}"
`)
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	ce := asCompositionError(err)
	if ce == nil {
		t.Fatalf("expected *CompositionError, got: %v", err)
	}
	if ce.Stage != "load_config" {
		t.Errorf("Stage = %q, want %q", ce.Stage, "load_config")
	}
	if !strings.Contains(ce.Reason, "failed to load configuration") {
		t.Errorf("Reason = %q, want it to contain %q", ce.Reason, "failed to load configuration")
	}
	if !strings.Contains(err.Error(), "vm.session_env") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "vm.session_env")
	}
}

// --- happy path tests ---

func TestCompose_SingleEnabled_NoDefault_AutoInfersDefault(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  enabled:
    - claude
`)
	result, err := composeEngine().Compose(path, testRegistry("claude"))
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	if result.ActiveProvider.Name() != "claude" {
		t.Errorf("ActiveProvider.Name() = %q, want %q", result.ActiveProvider.Name(), "claude")
	}
	if len(result.EnabledAgentDefs) != 1 {
		t.Errorf("EnabledAgentDefs has %d entries, want 1", len(result.EnabledAgentDefs))
	}
	if _, ok := result.EnabledAgentDefs["claude"]; !ok {
		t.Errorf("EnabledAgentDefs missing \"claude\"")
	}
}

func TestCompose_MultipleEnabled_WithDefault_HappyPath(t *testing.T) {
	t.Parallel()
	path := writeYAML(t, `
agents:
  default: claude
  enabled:
    - claude
    - copilot
`)
	result, err := composeEngine().Compose(path, testRegistry("claude", "copilot"))
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	if result.ActiveProvider.Name() != "claude" {
		t.Errorf("ActiveProvider.Name() = %q, want %q", result.ActiveProvider.Name(), "claude")
	}
	if len(result.EnabledAgentDefs) != 2 {
		t.Errorf("EnabledAgentDefs has %d entries, want 2", len(result.EnabledAgentDefs))
	}
	if _, ok := result.EnabledAgentDefs["claude"]; !ok {
		t.Errorf("EnabledAgentDefs missing \"claude\"")
	}
	if _, ok := result.EnabledAgentDefs["copilot"]; !ok {
		t.Errorf("EnabledAgentDefs missing \"copilot\"")
	}
}

func TestCompose_DefineWithoutEnabled_Excluded(t *testing.T) {
	t.Parallel()
	// opencode is defined but NOT in agents.enabled — it must not appear in EnabledAgentDefs.
	path := writeYAML(t, `
agents:
  default: claude
  enabled:
    - claude
  define:
    opencode:
      cli_command: opencode
`)
	result, err := composeEngine().Compose(path, testRegistry("claude", "opencode"))
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	if len(result.EnabledAgentDefs) != 1 {
		t.Errorf("EnabledAgentDefs = %v, want only claude", result.EnabledAgentDefs)
	}
	if _, ok := result.EnabledAgentDefs["opencode"]; ok {
		t.Errorf("EnabledAgentDefs contains agent \"opencode\" not in agents.enabled")
	}
}

func TestCompose_ActiveAgentDefMatchesDefault(t *testing.T) {
	t.Parallel()
	// ActiveAgentDef must be the merged def for the default agent.
	path := writeYAML(t, `
agents:
  default: copilot
  enabled:
    - copilot
  define:
    copilot:
      cli_command: "my-custom-copilot-cmd"
`)
	result, err := composeEngine().Compose(path, testRegistry("copilot"))
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	if result.ActiveAgentDef.CLICommand != "my-custom-copilot-cmd" {
		t.Errorf("ActiveAgentDef.CLICommand = %q, want %q",
			result.ActiveAgentDef.CLICommand, "my-custom-copilot-cmd")
	}
}

func TestCompose_UserAgentOverrideMerged(t *testing.T) {
	t.Parallel()
	// User can override built-in agent fields via agents.define.
	path := writeYAML(t, `
agents:
  enabled:
    - claude
  define:
    claude:
      cli_command: claude-override
      launch_args: --version
`)
	result, err := composeEngine().Compose(path, testRegistry("claude"))
	if err != nil {
		t.Fatalf("Compose: unexpected error: %v", err)
	}
	def := result.EnabledAgentDefs["claude"]
	if def.CLICommand != "claude-override" {
		t.Errorf("EnabledAgentDefs[claude].CLICommand = %q, want %q",
			def.CLICommand, "claude-override")
	}
	if def.LaunchArgs != "--version" {
		t.Errorf("EnabledAgentDefs[claude].LaunchArgs = %q, want %q",
			def.LaunchArgs, "--version")
	}
}

func TestCompose_ErrorMessage_IncludesExample(t *testing.T) {
	t.Parallel()
	// The "no agents enabled" error message must include a YAML example for users.
	path := writeYAML(t, "")
	_, err := composeEngine().Compose(path, testRegistry("claude"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "enabled:") {
		t.Errorf("error message should include YAML example with enabled:, got: %s", msg)
	}
}
