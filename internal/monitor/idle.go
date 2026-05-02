package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	aivmlog "aivm/internal/log"
	"aivm/internal/mcp"
	"aivm/internal/session"
	"aivm/internal/vm"
)

type IdleMonitor struct {
	Sessions     *session.Store
	VM           vm.VM
	MCP          *mcp.Manager
	Timeout      time.Duration
	PollInterval time.Duration
	PIDFile      string
}

func NewIdleMonitor(sessions *session.Store, v vm.VM, m *mcp.Manager, timeout time.Duration, stateDir string) *IdleMonitor {
	return &IdleMonitor{
		Sessions:     sessions,
		VM:           v,
		MCP:          m,
		Timeout:      timeout,
		PollInterval: 30 * time.Second,
		PIDFile:      filepath.Join(stateDir, "idle-monitor.pid"),
	}
}

func (m *IdleMonitor) EnsureRunning() error {
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
	proc.Release()
	aivmlog.Info("idle monitor started (pid=%d)", proc.Pid)
	return nil
}

func (m *IdleMonitor) Run(ctx context.Context) error {
	os.WriteFile(m.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(m.PIDFile)

	aivmlog.Info("idle monitor started (pid=%d, timeout=%s)", os.Getpid(), m.Timeout)

	ticker := time.NewTicker(m.PollInterval)
	defer ticker.Stop()

	var idleSince time.Time

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			status, err := m.VM.Status(ctx)
			if err != nil || status != vm.StatusRunning {
				aivmlog.Info("VM is no longer running — idle monitor exiting")
				m.MCP.Stop(ctx)
				return nil
			}

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
			aivmlog.Debug("idle for %s, shutdown in %s", idle.Round(time.Second), remaining.Round(time.Second))

			if idle >= m.Timeout {
				return m.shutdown(ctx)
			}
		}
	}
}

func (m *IdleMonitor) shutdown(ctx context.Context) error {
	aivmlog.Step("Idle timeout reached — initiating shutdown")
	if err := m.VM.Stop(ctx); err != nil {
		aivmlog.Warn("VM stop error: %v", err)
	}
	if err := m.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("aivm shutdown complete")
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
	proc.Signal(os.Interrupt)
	os.Remove(m.PIDFile)
}
