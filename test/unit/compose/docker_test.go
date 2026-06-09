package compose_test

import (
	"strings"
	"testing"

	"github.com/sisimomo/aivm/internal/compose"
)

func TestHostDockerRuntimeNotFoundError(t *testing.T) {
	msg := compose.HostDockerRuntimeNotFoundError().Error()
	if strings.Contains(strings.ToLower(msg), "colima") {
		t.Errorf("error should not mention Colima: %q", msg)
	}
	for _, want := range []string{"Docker Desktop", "OrbStack"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to mention %q", msg, want)
		}
	}
}
