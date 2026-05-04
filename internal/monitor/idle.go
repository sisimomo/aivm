package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/mcp"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/vm"
)

type IdleMonitor struct {
	Sessions      *session.Store
	VM            vm.VM
	MCP           mcp.MCPManager
	Timeout       time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration
	PIDFile       string
	StateDir      string
	// DisableDaemonLaunch prevents EnsureRunning from spawning subprocess daemons.
	// Set to true in tests where the monitor is driven in-process via RunMonitorInProcess instead.
	DisableDaemonLaunch bool
}

func NewIdleMonitor(sessions *session.Store, v vm.VM, m mcp.MCPManager, timeout, deleteTimeout time.Duration, stateDir string) *IdleMonitor {
	return &IdleMonitor{
		Sessions:      sessions,
		VM:            v,
		MCP:           m,
		Timeout:       timeout,
		DeleteTimeout: deleteTimeout,
		PollInterval:  30 * time.Second,
		PIDFile:       filepath.Join(stateDir, "idle-monitor.pid"),
		StateDir:      stateDir,
	}
}

func (m *IdleMonitor) EnsureRunning() error {
	if m.DisableDaemonLaunch {
		return nil
	}
	if m.isRunning() {
		aivmlog.Debug("idle monitor already running")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	proc, err := os.StartProcess(exe, []string{exe, "__monitor"},
		&os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
			Sys:   daemonSysProcAttr(),
		})
	if err != nil {
		return err
	}
	_ = proc.Release()
	aivmlog.Info("idle monitor started (pid=%d)", proc.Pid)
	return nil
}

func (m *IdleMonitor) Run(ctx context.Context) error {
	os.WriteFile(m.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(m.PIDFile)

	aivmlog.Info("idle monitor started (pid=%d, stop_timeout=%s, delete_timeout=%s)",
		os.Getpid(), m.Timeout, m.DeleteTimeout)

	ticker := time.NewTicker(m.PollInterval)
	defer ticker.Stop()

	var idleSince time.Time

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			status, err := m.VM.Status(ctx)
			if err != nil {
				aivmlog.Warn("VM status error: %v", err)
				continue
			}

			switch status {
			case vm.StatusRunning:
				// Phase 1: monitor for idle sessions; stop VM when idle long enough.
				m.Sessions.ClearVMStoppedAt()

				active, err := m.Sessions.CountActive()
				if err != nil {
					aivmlog.Warn("session count error: %v", err)
					continue
				}

				if active > 0 {
					idleSince = time.Time{}
					aivmlog.Debug("active sessions: %d", active)
					continue
				}

				if idleSince.IsZero() {
					idleSince = time.Now()
					m.Sessions.WriteLastActive()
				}

				idle := time.Since(idleSince)
				remaining := m.Timeout - idle
				aivmlog.Debug("idle for %s, stop in %s", idle.Round(time.Second), remaining.Round(time.Second))

				if idle >= m.Timeout {
					aivmlog.Step("Idle timeout reached — stopping VM and MCPJungle (Phase 1)")
					if err := m.VM.Stop(ctx); err != nil {
						aivmlog.Warn("VM stop error: %v", err)
					}
					if err := m.MCP.Stop(ctx); err != nil {
						aivmlog.Warn("MCPJungle stop error: %v", err)
					}
					m.Sessions.WriteVMStoppedAt()
					idleSince = time.Time{}
					aivmlog.Success("VM and MCPJungle stopped — will delete VM after %s if not resumed", m.DeleteTimeout)
				}

			case vm.StatusStopped:
				// Phase 2: VM is suspended; destroy after DeleteTimeout if not resumed.
				idleSince = time.Time{}

				stoppedAt := m.Sessions.ReadVMStoppedAt()
				if stoppedAt.IsZero() {
					// VM was stopped manually (not by Phase 1); do not auto-delete.
					aivmlog.Debug("VM stopped externally — skipping Phase 2 deletion")
					continue
				}

				elapsed := time.Since(stoppedAt)
				remaining := m.DeleteTimeout - elapsed
				aivmlog.Debug("VM suspended for %s, deletion in %s", elapsed.Round(time.Second), remaining.Round(time.Second))

				if elapsed >= m.DeleteTimeout {
					return m.destroy(ctx)
				}

			case vm.StatusNotFound:
				// VM already gone — nothing left to monitor.
				aivmlog.Info("VM no longer exists — idle monitor exiting")
				_ = m.MCP.Stop(ctx)
				return nil
			}
		}
	}
}

func (m *IdleMonitor) destroy(ctx context.Context) error {
	aivmlog.Step("VM suspension timeout reached — deleting VM (Phase 2)")
	if err := m.VM.Destroy(ctx); err != nil {
		aivmlog.Warn("VM destroy error: %v", err)
	}
	m.Sessions.ClearVMStoppedAt()
	if err := m.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("VM deleted — resources reclaimed")
	return nil
}

func (m *IdleMonitor) isRunning() bool {
	data, err := os.ReadFile(m.PIDFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (m *IdleMonitor) Stop() {
	data, err := os.ReadFile(m.PIDFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(os.Interrupt)
	os.Remove(m.PIDFile)
}

