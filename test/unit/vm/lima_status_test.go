package vm_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestParseLimaListStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		lines   []string
		profile string
		want    vm.Status
	}{
		{
			name: "running",
			lines: []string{
				"NAME       STATUS     SSH            VMTYPE    ARCH",
				"aivm       Running    127.0.0.1:22   vz        aarch64",
			},
			profile: "aivm",
			want:    vm.StatusRunning,
		},
		{
			name: "stopped",
			lines: []string{
				"aivm       Stopped    127.0.0.1:22   vz        aarch64",
			},
			profile: "aivm",
			want:    vm.StatusStopped,
		},
		{
			name:    "not found",
			lines:   []string{"other      Running"},
			profile: "aivm",
			want:    vm.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := vm.ParseLimaListStatus(tc.lines, tc.profile)
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
