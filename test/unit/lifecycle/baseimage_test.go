package lifecycle_test

import (
	"context"
	"testing"

	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/lifecycle"
	"github.com/sisimomo/aivm/internal/vm"
)

func TestSaveBaseImageBestEffort_Disabled(t *testing.T) {
	t.Parallel()
	svc := &lifecycle.LifecycleService{
		Config: &config.Config{
			StateDir: t.TempDir(),
			VM: config.VMConfig{
				BaseImageEnable: false,
			},
		},
	}
	if err := svc.SaveBaseImageBestEffort(context.Background(), vm.StartOptions{}); err != nil {
		t.Fatal(err)
	}
}
