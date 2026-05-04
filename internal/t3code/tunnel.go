package t3code

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Tunnel is the production Manager implementation. It starts an SSH
// port-forward from the host to the Colima VM and tracks the SSH process by
// PID file so it can be stopped by a later `aivm stop` invocation.
type Tunnel struct {
	// Profile is the Colima VM profile name (e.g. "default").
	Profile string
	// StateDir is the aivm state directory where the PID file is written.
	StateDir string
}

const pidFileName = "t3code-tunnel.pid"

// Launch starts an SSH port-forward tunnel from localhost:<port> on the host
// to localhost:<port> inside the VM. The SSH process is detached (PID recorded
// in StateDir) so that aivm launch can return immediately.
func (t *Tunnel) Launch(_ context.Context, port int) error {
	if t.IsRunning() {
		return nil
	}

	sshConfig, sshHost := t.sshCoords()
	portFwd := fmt.Sprintf("%d:localhost:%d", port, port)
	cmd := exec.Command("ssh", "-N", "-L", portFwd, "-F", sshConfig, sshHost)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting SSH tunnel: %w", err)
	}

	pid := cmd.Process.Pid
	// Release detaches us from the process — it keeps running independently.
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("releasing SSH tunnel process: %w", err)
	}

	pidFile := filepath.Join(t.StateDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("writing tunnel PID file: %w", err)
	}

	return nil
}

// Stop kills the SSH tunnel process identified by the PID file. Non-fatal if
// the process is already gone.
func (t *Tunnel) Stop() error {
	pid, err := t.readPID()
	if err != nil || pid == 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	_ = proc.Signal(syscall.SIGTERM)

	pidFile := filepath.Join(t.StateDir, pidFileName)
	_ = os.Remove(pidFile)
	return nil
}

// IsRunning reports whether the SSH tunnel is currently active by checking the
// PID file and sending a null signal to the process.
func (t *Tunnel) IsRunning() bool {
	pid, err := t.readPID()
	if err != nil || pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (t *Tunnel) readPID() (int, error) {
	pidFile := filepath.Join(t.StateDir, pidFileName)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content: %w", err)
	}
	return pid, nil
}

func (t *Tunnel) sshCoords() (sshConfig, sshHost string) {
	home, _ := os.UserHomeDir()
	colimaHome := os.Getenv("COLIMA_HOME")
	if colimaHome == "" {
		colimaHome = filepath.Join(home, ".colima")
	}
	sshConfig = filepath.Join(colimaHome, "_lima", "colima-"+t.Profile, "ssh.config")
	sshHost = "lima-colima-" + t.Profile
	return
}
