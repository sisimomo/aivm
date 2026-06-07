package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/sisimomo/aivm/internal/compose"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/vm"
)

type IdleMonitor struct {
	Sessions      *session.Store
	VM            vm.VM
	Compose       compose.ComposeManager
	Timeout       time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration
	PIDFile       string
	StateDir      string
	// DisableDaemonLaunch prevents EnsureRunning from spawning subprocess daemons.
	// Set to true in tests where the monitor is driven in-process via RunMonitorInProcess instead.
	DisableDaemonLaunch bool
}

func NewIdleMonitor(sessions *session.Store, v vm.VM, s compose.ComposeManager, timeout, deleteTimeout time.Duration, stateDir string) *IdleMonitor {
	return &IdleMonitor{
		Sessions:      sessions,
		VM:            v,
		Compose:       s,
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
		slog.Debug("idle monitor already running")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	proc, err := os.StartProcess(exe, []string{exe, "__monitor"},
		&os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
			Sys:   &syscall.SysProcAttr{Setsid: true},
		})
	if err != nil {
		return err
	}
	_ = proc.Release()
	slog.Debug(fmt.Sprintf("idle monitor started (pid=%d)", proc.Pid))
	return nil
}

func (m *IdleMonitor) Run(ctx context.Context) error {
	if err := aivmlog.UseDedicatedLog(m.StateDir, "idle-monitor"); err != nil {
		slog.Warn(fmt.Sprintf("idle monitor: dedicated log file: %v", err))
	}

	if err := os.WriteFile(m.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		slog.Warn(fmt.Sprintf("idle monitor: write pid file: %v", err))
	}
	defer os.Remove(m.PIDFile)

	slog.Debug(fmt.Sprintf("idle monitor started (pid=%d, stop_timeout=%s, delete_timeout=%s)",
		os.Getpid(), m.Timeout, m.DeleteTimeout))

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
				slog.Warn(fmt.Sprintf("VM status error: %v", err))
				continue
			}

			switch status {
			case vm.StatusRunning:
				// Phase 1: monitor for idle sessions; stop VM when idle long enough.
				m.Sessions.ClearVMStoppedAt()

				active, err := m.Sessions.CountActive()
				if err != nil {
					slog.Warn(fmt.Sprintf("session count error: %v", err))
					continue
				}

				if active > 0 {
					idleSince = time.Time{}
					slog.Log(context.Background(), aivmlog.SlogTrace, fmt.Sprintf("active sessions: %d", active))
					continue
				}

				if idleSince.IsZero() {
					idleSince = time.Now()
					m.Sessions.WriteLastActive()
				}

				idle := time.Since(idleSince)
				remaining := m.Timeout - idle
				slog.Log(context.Background(), aivmlog.SlogTrace, fmt.Sprintf("idle for %s, stop in %s", idle.Round(time.Second), remaining.Round(time.Second)))

				if idle >= m.Timeout {
					slog.Info("Idle timeout reached — stopping VM and compose services (Phase 1)")
					if err := m.VM.Stop(ctx); err != nil {
						slog.Warn(fmt.Sprintf("VM stop error: %v", err))
						continue
					}
					if err := m.Compose.Down(ctx); err != nil {
						slog.Warn(fmt.Sprintf("compose stop error: %v", err))
					}
					m.Sessions.WriteVMStoppedAt()
					idleSince = time.Time{}
					slog.Info(fmt.Sprintf("VM and compose services stopped — will delete VM after %s if not resumed", m.DeleteTimeout))
				}

			case vm.StatusStopped:
				// Phase 2: VM is suspended; destroy after DeleteTimeout if not resumed.
				idleSince = time.Time{}

				stoppedAt := m.Sessions.ReadVMStoppedAt()
				if stoppedAt.IsZero() {
					// VM was stopped manually (not by Phase 1); do not auto-delete.
					slog.Debug("VM stopped externally — skipping Phase 2 deletion")
					continue
				}

				elapsed := time.Since(stoppedAt)
				remaining := m.DeleteTimeout - elapsed
				slog.Log(context.Background(), aivmlog.SlogTrace, fmt.Sprintf("VM suspended for %s, deletion in %s", elapsed.Round(time.Second), remaining.Round(time.Second)))

				if elapsed >= m.DeleteTimeout {
					return m.destroy(ctx)
				}

			case vm.StatusNotFound:
				// VM already gone — nothing left to monitor.
				slog.Debug("VM no longer exists — idle monitor exiting")
				if err := m.Compose.Down(ctx); err != nil {
					return fmt.Errorf("tearing down orphaned compose services: %w", err)
				}
				return nil
			}
		}
	}
}

func (m *IdleMonitor) destroy(ctx context.Context) error {
	slog.Info("VM suspension timeout reached — deleting VM (Phase 2)")
	if err := m.VM.Destroy(ctx); err != nil {
		slog.Warn(fmt.Sprintf("VM destroy error: %v", err))
	}
	m.Sessions.ClearVMStoppedAt()
	if err := m.Compose.Down(ctx); err != nil {
		slog.Warn(fmt.Sprintf("compose destroy error: %v", err))
	}
	slog.Info("VM deleted — resources reclaimed")
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
