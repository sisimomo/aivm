package cli_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/cli"
)

func TestLogLevelFromArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{
			name: "before agent dash",
			args: []string{"agent", "--log-level", "error", "--", "--version"},
			want: "error",
			ok:   true,
		},
		{
			name: "equals form",
			args: []string{"--log-level=debug", "start"},
			want: "debug",
			ok:   true,
		},
		{
			name: "after agent dash ignored",
			args: []string{"agent", "--", "--log-level", "error"},
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
			got, ok := cli.LogLevelFromArgs(tc.args)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
