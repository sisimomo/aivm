package vm_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestBuildRunScript_ExecsCLIWithEscapedArgs(t *testing.T) {
	t.Parallel()
	script := vm.BuildRunScript("/work", "agent", []string{"-p", "hello world"})
	if !strings.Contains(script, "cd '/work'") {
		t.Fatalf("script missing cd: %s", script)
	}
	if !strings.Contains(script, "exec 'agent' '-p' 'hello world'") {
		t.Fatalf("script missing exec line: %s", script)
	}
}

func TestBuildLaunchScript_IncludesLaunchArgs(t *testing.T) {
	t.Parallel()
	script := vm.BuildLaunchScript("/work", "agent", "--yolo")
	if !strings.Contains(script, "exec 'agent' --yolo") {
		t.Fatalf("script missing launch args: %s", script)
	}
}
