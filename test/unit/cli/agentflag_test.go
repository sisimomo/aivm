package cli_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/cli"
)

func TestAgentFromArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{
			name: "before agent subcommand",
			args: []string{"--agent", "opencode", "agent", "--", "--version"},
			want: "opencode",
			ok:   true,
		},
		{
			name: "equals form",
			args: []string{"--agent=claude", "agent", "--", "-p", "hi"},
			want: "claude",
			ok:   true,
		},
		{
			name: "between agent and dash",
			args: []string{"agent", "--agent", "cursor", "--", "--version"},
			want: "cursor",
			ok:   true,
		},
		{
			name: "after agent dash ignored",
			args: []string{"agent", "--", "--agent", "opencode"},
			ok:   false,
		},
		{
			name: "missing",
			args: []string{"agent", "--", "--version"},
			ok:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cli.AgentFromArgs(tc.args)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
