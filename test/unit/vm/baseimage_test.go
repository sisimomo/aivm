package vm_test

import (
	"testing"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestDockerBaseImageTag(t *testing.T) {
	t.Parallel()
	if got := vm.DockerBaseImageTag("dev"); got != "aivm-dev-base" {
		t.Fatalf("got %q", got)
	}
}

func TestLimaShadowProfile(t *testing.T) {
	t.Parallel()
	if got := vm.LimaShadowProfile("aivm"); got != "aivm-base" {
		t.Fatalf("got %q", got)
	}
}
