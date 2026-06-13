package lifecycle_test

import (
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
)

func TestEvaluateTimers_BootstrapOnlyDue(t *testing.T) {
	t.Parallel()

	got := lifecycle.EvaluateTimers(
		2*time.Hour,
		24*time.Hour,
		48*time.Hour,
		24*time.Hour,
	)
	if !got.BootstrapDue {
		t.Fatal("expected bootstrap timer due")
	}
	if got.VMAgeDue {
		t.Fatal("expected VM timer not due")
	}
}

func TestEvaluateTimers_BothDue(t *testing.T) {
	t.Parallel()

	got := lifecycle.EvaluateTimers(
		48*time.Hour,
		24*time.Hour,
		72*time.Hour,
		24*time.Hour,
	)
	if !got.BootstrapDue || !got.VMAgeDue {
		t.Fatalf("expected both timers due, got %#v", got)
	}
}

func TestEvaluateTimers_MissingBootstrapAt_NotDue(t *testing.T) {
	t.Parallel()

	got := lifecycle.EvaluateTimers(
		10*time.Hour,
		24*time.Hour,
		lifecycle.BootstrapAgeUnknown,
		24*time.Hour,
	)
	if got.BootstrapDue {
		t.Fatal("expected unknown bootstrap age to not be due")
	}
}

func TestRuntimeChanged_DetectsBackendMismatch(t *testing.T) {
	t.Parallel()

	svc := &lifecycle.LifecycleService{
		Config: &config.Config{
			VM: config.VMConfig{
				Backend: "docker",
				Type:    "qemu",
			},
		},
	}
	state := &lifecycle.BootstrapState{
		Backend: "lima",
		VMType:  "qemu",
	}
	if !lifecycle.RuntimeChangedForTest(svc, state) {
		t.Fatal("expected runtime change for backend mismatch")
	}
}
