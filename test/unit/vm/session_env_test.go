package vm_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestBuildDockerSSHCmd_ExportsConfiguredVars(t *testing.T) {
	t.Parallel()
	got := vm.BuildDockerSSHCmd(map[string]string{
		"MY_TOOL_SESSION_ID": "abc-123",
		"CI_JOB_ID":          "job-9",
	})
	if !strings.Contains(got, "export MY_TOOL_SESSION_ID='abc-123'") {
		t.Fatalf("got %q, want MY_TOOL_SESSION_ID export", got)
	}
	if !strings.Contains(got, "export CI_JOB_ID='job-9'") {
		t.Fatalf("got %q, want CI_JOB_ID export", got)
	}
	if !strings.HasSuffix(got, "exec bash") {
		t.Fatalf("got %q, want exec bash suffix", got)
	}
}

func TestBuildDockerSSHCmd_EmptyEnv(t *testing.T) {
	t.Parallel()
	got := vm.BuildDockerSSHCmd(nil)
	if got != "exec bash" {
		t.Fatalf("got %q, want %q", got, "exec bash")
	}
}

func TestBuildDockerSSHCmd_ExportsEmptyValues(t *testing.T) {
	t.Parallel()
	got := vm.BuildDockerSSHCmd(map[string]string{
		"UNSET_ON_HOST": "",
	})
	if !strings.Contains(got, "export UNSET_ON_HOST=''") {
		t.Fatalf("got %q, want empty export", got)
	}
}

func TestBuildDockerSSHCmd_EscapesSingleQuotes(t *testing.T) {
	t.Parallel()
	got := vm.BuildDockerSSHCmd(map[string]string{
		"TOKEN": "it's-fine",
	})
	want := "export TOKEN='it'\"'\"'s-fine'"
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want escaped export %q", got, want)
	}
}

func TestSSHScriptWithEnv_ExportsBeforeScript(t *testing.T) {
	t.Parallel()
	got := vm.SSHScriptWithEnv(map[string]string{
		"MY_TOOL_SESSION_ID": "sess-1",
	}, "exec bash -l")
	if !strings.HasPrefix(got, "export MY_TOOL_SESSION_ID='sess-1'; ") {
		t.Fatalf("got %q", got)
	}
	if !strings.HasSuffix(got, "exec bash -l") {
		t.Fatalf("got %q", got)
	}
}

func TestSSHScriptWithEnv_EmptyEnvReturnsScriptUnchanged(t *testing.T) {
	t.Parallel()
	script := "echo hi"
	if got := vm.SSHScriptWithEnv(nil, script); got != script {
		t.Fatalf("got %q, want %q", got, script)
	}
}
