package config

import (
	"os"
	"testing"
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
		if err := ValidateEnvVarName(name); err != nil {
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
		err := ValidateEnvVarName(tc.name)
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

func TestResolvedEnv_Nil(t *testing.T) {
	t.Parallel()
	vm := &VMConfig{}
	if got := vm.ResolvedEnv(); got != nil {
		t.Errorf("ResolvedEnv() with nil Env: got %v, want nil", got)
	}
}

func TestResolvedEnv_Empty(t *testing.T) {
	t.Parallel()
	vm := &VMConfig{Env: map[string]string{}}
	if got := vm.ResolvedEnv(); got != nil {
		t.Errorf("ResolvedEnv() with empty Env: got %v, want nil", got)
	}
}

func TestResolvedEnv_LiteralValues(t *testing.T) {
	t.Parallel()
	vm := &VMConfig{Env: map[string]string{
		"FOO": "bar",
		"BAZ": "qux",
	}}
	got := vm.ResolvedEnv()
	if got["FOO"] != "bar" {
		t.Errorf("FOO: got %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ: got %q, want %q", got["BAZ"], "qux")
	}
}

func TestResolvedEnv_ExpandsHostVar(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_HOST", "expanded_value")
	vm := &VMConfig{Env: map[string]string{
		"MY_VAR": "${AIVM_UNIT_TEST_HOST}",
	}}
	got := vm.ResolvedEnv()
	if got["MY_VAR"] != "expanded_value" {
		t.Errorf("MY_VAR: got %q, want %q", got["MY_VAR"], "expanded_value")
	}
}

func TestResolvedEnv_MissingHostVarExpandsToEmpty(t *testing.T) {
	os.Unsetenv("AIVM_UNIT_TEST_MISSING")
	vm := &VMConfig{Env: map[string]string{
		"MY_VAR": "${AIVM_UNIT_TEST_MISSING}",
	}}
	got := vm.ResolvedEnv()
	if got["MY_VAR"] != "" {
		t.Errorf("MY_VAR with missing host var: got %q, want empty string", got["MY_VAR"])
	}
}

func TestResolvedEnv_OriginalMapUnmodified(t *testing.T) {
	t.Setenv("AIVM_UNIT_TEST_ORIG", "resolved")
	original := map[string]string{"V": "${AIVM_UNIT_TEST_ORIG}"}
	vm := &VMConfig{Env: original}
	vm.ResolvedEnv()
	if original["V"] != "${AIVM_UNIT_TEST_ORIG}" {
		t.Errorf("original map was mutated: got %q", original["V"])
	}
}
