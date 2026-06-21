package e2e

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
	"github.com/sisimomo/aivm/test/framework"
	"github.com/sisimomo/aivm/test/framework/actions"
	"github.com/sisimomo/aivm/test/framework/assertions"
	"github.com/sisimomo/aivm/test/framework/conditions"
)

func TestSocketBridge_DockerRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	t.Parallel()
	skipUnlessDocker(t)

	home, _ := os.UserHomeDir()
	sockDir := filepath.Join(home, ".aivm", "test-sockets", t.Name())
	hostSock := filepath.Join(sockDir, "echo.sock")
	guestSock := "/run/aivm/sockets/echo.sock"

	_ = os.Remove(hostSock)
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", hostSock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hostSock, 0o666); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close(); os.Remove(hostSock) })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = conn.Write([]byte("pong"))
			conn.Close()
		}
	}()

	h := framework.New(t, framework.WithSocketBridge(hostSock, guestSock))
	h.Scenario("socket bridge round-trip").
		Step("Start VM", actions.CLI("start")).
		Wait("VM is running", conditions.VMStatus(vm.StatusRunning), 5*time.Minute).
		Assert("Guest path is socket", assertions.VMRunOutput(
			"test -S "+guestSock+" && echo socket-ok", "socket-ok")).
		Assert("Guest can connect", assertions.VMRunOutput(
			`python3 -c "import socket;s=socket.socket(socket.AF_UNIX);s.connect('`+guestSock+`');print(s.recv(8).decode())"`,
			"pong")).
		Run()
}
