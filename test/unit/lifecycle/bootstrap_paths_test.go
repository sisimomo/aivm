package lifecycle_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/monitor"
	"github.com/sisimomo/aivm/internal/vm"
)

type captureVM struct {
	scripts []string
}

func (c *captureVM) Profile() string { return "test" }

func (c *captureVM) NeedsPortBindingAtBoot() bool { return false }

func (c *captureVM) Status(_ context.Context) (vm.Status, error) { return vm.StatusRunning, nil }

func (c *captureVM) Start(_ context.Context, _ vm.StartOptions) error { return nil }

func (c *captureVM) Stop(_ context.Context) error { return nil }

func (c *captureVM) Destroy(_ context.Context) error { return nil }

func (c *captureVM) Run(_ context.Context, script string, _ map[string]string) error {
	c.scripts = append(c.scripts, script)
	return nil
}

func (c *captureVM) RunOutput(_ context.Context, script string, _ map[string]string) (string, error) {
	c.scripts = append(c.scripts, script)
	return "", nil
}

func (c *captureVM) RunInteractive(_ context.Context, script string, _ map[string]string) error {
	c.scripts = append(c.scripts, script)
	return nil
}

func (c *captureVM) RunStream(_ context.Context, script string, _ map[string]string) (int, error) {
	c.scripts = append(c.scripts, script)
	return 0, nil
}

func (c *captureVM) SSH(_ context.Context, _ map[string]string) error { return nil }

func (c *captureVM) CopyTo(_ context.Context, _, _ string, _ bool) error { return nil }

func (c *captureVM) CopyFrom(_ context.Context, _, _ string, _ bool) error { return nil }

func (c *captureVM) WaitReady(_ context.Context, _ time.Duration) error { return nil }

func (c *captureVM) GetPublishedPort(_ int) (int, error) { return 0, nil }

type failingDestroyVM struct {
	captureVM
	destroyCalled bool
}

func (f *failingDestroyVM) Destroy(_ context.Context) error {
	f.destroyCalled = true
	return context.Canceled
}

type noopCompose struct{}

func (noopCompose) Up(_ context.Context) error { return nil }

func (noopCompose) Down(_ context.Context) error { return nil }

func (noopCompose) HealthMap(_ context.Context) map[string]bool { return nil }

func TestApplyPostRestore_WritesEnv(t *testing.T) {
	t.Parallel()
	v := &captureVM{}
	stateDir := t.TempDir()
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{
			StateDir: stateDir,
			VM: config.VMConfig{
				Env: map[string]string{"TEST_VAR": "hello"},
			},
		},
		VM: v,
	}
	if err := lifecycle.ApplyPostRestoreForTest(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if len(v.scripts) == 0 {
		t.Fatal("expected Run to be called")
	}
	found := false
	for _, script := range v.scripts {
		if strings.Contains(script, "aivm-user-env.sh") && strings.Contains(script, "base64 -d") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected env script writing aivm-user-env.sh via base64, got scripts: %v", v.scripts)
	}
}

func TestFastRecreate_InvalidBase_FallsBack(t *testing.T) {
	t.Parallel()
	v := &failingDestroyVM{}
	stateDir := t.TempDir()
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{
			StateDir: stateDir,
			VM: config.VMConfig{
				BaseImageEnable: true,
			},
		},
		VM:      v,
		Monitor: &monitor.IdleMonitor{StateDir: stateDir},
		Compose: noopCompose{},
	}
	err := lifecycle.FastRecreateForTest(context.Background(), svc)
	if err == nil {
		t.Fatal("expected error from full bootstrap fallback")
	}
	if !v.destroyCalled {
		t.Fatal("expected fullBootstrap fallback to call Destroy")
	}
}
