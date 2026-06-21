package vm_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

func TestLimaTemplate_ValidatesWithLimactl(t *testing.T) {
	if _, err := exec.LookPath("limactl"); err != nil {
		t.Skip("limactl not installed")
	}

	path, err := vm.LimaTemplatePath([]vm.Mount{
		{HostPath: "/tmp/aivm-host", GuestPath: "/work", Writable: true},
	}, nil)
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

func TestLimaTemplate_IncludesPortForwards(t *testing.T) {
	path, err := vm.LimaTemplatePath([]vm.Mount{}, []vm.SocketBridge{{
		HostPath: "/tmp/host.sock", GuestPath: "/run/aivm/sockets/host.sock",
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "portForwards:") {
		t.Fatalf("missing portForwards in template:\n%s", body)
	}
	if !strings.Contains(body, "reverse: true") {
		t.Fatal("missing reverse: true")
	}
}
