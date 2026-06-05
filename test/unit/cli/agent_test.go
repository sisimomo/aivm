package cli_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/cli"
)

func TestAgentCmd_requiresDashBeforeAgentFlags(t *testing.T) {
	t.Parallel()
	root := cli.NewRootCmd("test", func(string) (*cli.App, error) {
		return nil, nil
	})
	root.SetArgs([]string{"agent", "-p", "noop"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing '--'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentCmd_requiresArgsAfterDash(t *testing.T) {
	t.Parallel()
	root := cli.NewRootCmd("test", func(string) (*cli.App, error) {
		return nil, nil
	})
	root.SetArgs([]string{"agent", "--"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no agent arguments after '--'") {
		t.Fatalf("unexpected error: %v", err)
	}
}
