package vm_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaTemplate_ValidatesWithLimactl(t *testing.T) {
	if _, err := exec.LookPath("limactl"); err != nil {
		t.Skip("limactl not installed")
	}

	path, err := vm.LimaTemplatePath()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "limactl", "template", "validate", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("limactl template validate: %v\n%s", err, out)
	}
}
