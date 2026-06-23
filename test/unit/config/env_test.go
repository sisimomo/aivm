package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
)

// --- ValidateEnvVarName ---

func TestValidateEnvVarName_Valid(t *testing.T) {
	t.Parallel()
	cases := []string{
		"FOO",
		"_BAR",
		"FOO_BAR",
		"A1B2",
		"_",
		"lowercase_ok",
		"Mixed_Case_123",
	}
	for _, name := range cases {
		if err := config.ValidateEnvVarName(name); err != nil {
			t.Errorf("ValidateEnvVarName(%q): unexpected error: %v", name, err)
		}
	}
}

func TestValidateEnvVarName_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		wantContain string
	}{
		{"", "empty"},
		{"1FOO", "digit"},
		{"FOO-BAR", "not allowed"},
		{"FOO BAR", "not allowed"},
		{"FOO.BAR", "not allowed"},
		{"$FOO", "not allowed"},
	}
	for _, tc := range cases {
		err := config.ValidateEnvVarName(tc.name)
		if err == nil {
			t.Errorf("ValidateEnvVarName(%q): expected error, got nil", tc.name)
			continue
		}
		if tc.wantContain != "" {
			msg := err.Error()
			found := false
			for i := 0; i <= len(msg)-len(tc.wantContain); i++ {
				if msg[i:i+len(tc.wantContain)] == tc.wantContain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("ValidateEnvVarName(%q) error = %q, want it to contain %q", tc.name, msg, tc.wantContain)
			}
		}
	}
}

// --- ResolvedEnv ---

func testConfigWithEnv(env map[string]string) *config.Config {
	home, _ := os.UserHomeDir()
	return &config.Config{
		StateDir: filepath.Join(home, ".aivm"),
		VM: config.VMConfig{
			Name: "testvm",
			Env:  env,
		},
	}
}

func TestResolvedEnv_Nil(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{}}
	if got := cfg.ResolvedEnv(); got != nil {
		t.Errorf("ResolvedEnv() with nil Env: got %v, want nil", got)
	}
}

func TestResolvedEnv_Empty(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{Env: map[string]string{}}}
	if got := cfg.ResolvedEnv(); got != nil {
		t.Errorf("ResolvedEnv() with empty Env: got %v, want nil", got)
	}
}

func TestResolvedEnv_LiteralValues(t *testing.T) {
	t.Parallel()
	cfg := testConfigWithEnv(map[string]string{
		"FOO": "bar",
		"BAZ": "qux",
	})
	got := cfg.ResolvedEnv()
	if got["FOO"] != "bar" {
		t.Errorf("FOO: got %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ: got %q, want %q", got["BAZ"], "qux")
	}
}

func TestResolvedEnv_ExpandsHostVar(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_HOST", "expanded_value")
	cfg := testConfigWithEnv(map[string]string{
		"MY_VAR": "${AIVM_UNIT_TEST_HOST}",
	})
	got := cfg.ResolvedEnv()
	if got["MY_VAR"] != "expanded_value" {
		t.Errorf("MY_VAR: got %q, want %q", got["MY_VAR"], "expanded_value")
	}
}

func TestResolvedEnv_MissingHostVarExpandsToEmpty(t *testing.T) {
	os.Unsetenv("AIVM_UNIT_TEST_MISSING")
	cfg := testConfigWithEnv(map[string]string{
		"MY_VAR": "${AIVM_UNIT_TEST_MISSING}",
	})
	got := cfg.ResolvedEnv()
	if got["MY_VAR"] != "" {
		t.Errorf("MY_VAR with missing host var: got %q, want empty string", got["MY_VAR"])
	}
}

func TestResolvedEnv_ExpandsHostHomeTemplate(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	cfg := testConfigWithEnv(map[string]string{
		"HERDR_SOCKET_PATH": `{{ .host_home }}/.config/herdr/herdr.sock`,
	})
	got := cfg.ResolvedEnv()
	want := filepath.Join(home, ".config/herdr/herdr.sock")
	if got["HERDR_SOCKET_PATH"] != want {
		t.Errorf("HERDR_SOCKET_PATH: got %q, want %q", got["HERDR_SOCKET_PATH"], want)
	}
}

func TestResolvedEnv_OriginalMapUnmodified(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_ORIG", "resolved")
	original := map[string]string{"V": "${AIVM_UNIT_TEST_ORIG}"}
	cfg := testConfigWithEnv(original)
	cfg.ResolvedEnv()
	if original["V"] != "${AIVM_UNIT_TEST_ORIG}" {
		t.Errorf("original map was mutated: got %q", original["V"])
	}
}

// --- ResolvedSessionEnv ---

func testConfigWithSessionEnv(sessionEnv map[string]string) *config.Config {
	home, _ := os.UserHomeDir()
	return &config.Config{
		StateDir: filepath.Join(home, ".aivm"),
		VM: config.VMConfig{
			Name:       "testvm",
			SessionEnv: sessionEnv,
		},
	}
}

func TestResolvedSessionEnv_Nil(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{}}
	if got := cfg.ResolvedSessionEnv(); got != nil {
		t.Errorf("ResolvedSessionEnv() with nil SessionEnv: got %v, want nil", got)
	}
}

func TestResolvedSessionEnv_Empty(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{VM: config.VMConfig{SessionEnv: map[string]string{}}}
	if got := cfg.ResolvedSessionEnv(); got != nil {
		t.Errorf("ResolvedSessionEnv() with empty SessionEnv: got %v, want nil", got)
	}
}

func TestResolvedSessionEnv_ExpandsHostVar(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_SESSION_HOST", "sess-42")
	cfg := testConfigWithSessionEnv(map[string]string{
		"MY_TOOL_SESSION_ID": "${AIVM_UNIT_TEST_SESSION_HOST}",
	})
	got := cfg.ResolvedSessionEnv()
	if got["MY_TOOL_SESSION_ID"] != "sess-42" {
		t.Errorf("MY_TOOL_SESSION_ID: got %q, want %q", got["MY_TOOL_SESSION_ID"], "sess-42")
	}
}

func TestResolvedSessionEnv_MissingHostVarExpandsToEmpty(t *testing.T) {
	const hostVar = "AIVM_UNIT_TEST_SESSION_MISSING"
	prev, had := os.LookupEnv(hostVar)
	os.Unsetenv(hostVar)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(hostVar, prev)
		} else {
			_ = os.Unsetenv(hostVar)
		}
	})
	cfg := testConfigWithSessionEnv(map[string]string{
		"CI_JOB_ID": "${" + hostVar + "}",
	})
	got := cfg.ResolvedSessionEnv()
	if got["CI_JOB_ID"] != "" {
		t.Errorf("CI_JOB_ID with missing host var: got %q, want empty string", got["CI_JOB_ID"])
	}
}

func TestResolvedSessionEnv_LiteralValue(t *testing.T) {
	t.Parallel()
	cfg := testConfigWithSessionEnv(map[string]string{
		"FIXED_FLAG": "always-on",
	})
	got := cfg.ResolvedSessionEnv()
	if got["FIXED_FLAG"] != "always-on" {
		t.Errorf("FIXED_FLAG: got %q, want %q", got["FIXED_FLAG"], "always-on")
	}
}

func TestResolvedSessionEnv_ExpandsHostHomeTemplate(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	cfg := testConfigWithSessionEnv(map[string]string{
		"HERDR_SOCKET_PATH": `{{ .host_home }}/.config/herdr/herdr.sock`,
	})
	got := cfg.ResolvedSessionEnv()
	want := filepath.Join(home, ".config/herdr/herdr.sock")
	if got["HERDR_SOCKET_PATH"] != want {
		t.Errorf("HERDR_SOCKET_PATH: got %q, want %q", got["HERDR_SOCKET_PATH"], want)
	}
}

func TestResolvedSessionEnv_OriginalMapUnmodified(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_SESSION_ORIG", "resolved")
	original := map[string]string{"V": "${AIVM_UNIT_TEST_SESSION_ORIG}"}
	cfg := testConfigWithSessionEnv(original)
	cfg.ResolvedSessionEnv()
	if original["V"] != "${AIVM_UNIT_TEST_SESSION_ORIG}" {
		t.Errorf("original map was mutated: got %q", original["V"])
	}
}
