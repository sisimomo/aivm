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
	Sessions      *session.Store
	VM            vm.VM
	MCP           mcp.MCPManager
	Timeout       time.Duration
	DeleteTimeout time.Duration
	PollInterval  time.Duration
	PIDFile       string
	StateDir      string
	// VMFactory creates VM instances for secondary profiles (e.g. the legacy VM
	// in RunLegacyMonitor). In production this is vm.NewColima; tests substitute
	// a mock factory via the test harness.
	VMFactory vm.VMFactory
	// DisableDaemonLaunch prevents EnsureRunning and EnsureLegacyMonitorRunning
	// from spawning subprocess daemons. Set to true in tests where the monitor
	// is driven in-process via RunMonitorInProcess instead.
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
	proc.Release()
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
				m.MCP.Stop(ctx)
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
	proc.Signal(os.Interrupt)
	os.Remove(m.PIDFile)
}

// EnsureLegacyMonitorRunning starts the legacy VM monitor daemon in the background.
// It is called after initiating a two-VM transition so that the old VM is automatically
// destroyed once all sessions that pre-date the transition have ended.
func (m *IdleMonitor) EnsureLegacyMonitorRunning() error {
	if m.DisableDaemonLaunch {
		return nil
	}
	pidFile := filepath.Join(m.StateDir, "legacy-monitor.pid")
	if data, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					aivmlog.Debug("legacy monitor already running")
					return nil
				}
			}
		}
		os.Remove(pidFile)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	proc, err := os.StartProcess(exe, []string{exe, "__legacy-monitor"},
		&os.ProcAttr{
			Files: []*os.File{nil, nil, nil},
			Sys:   daemonSysProcAttr(),
		})
	if err != nil {
		return err
	}
	proc.Release()
	aivmlog.Info("legacy monitor started (pid=%d)", proc.Pid)
	return nil
}

// RunLegacyMonitor polls until all sessions that pre-date the transition have ended,
// then destroys the legacy VM and clears the transition state.
func (m *IdleMonitor) RunLegacyMonitor(ctx context.Context, ts *vm.TransitionState) error {
	pidFile := filepath.Join(m.StateDir, "legacy-monitor.pid")
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(pidFile)

	legacyVM := m.VMFactory(ts.LegacyProfile, m.StateDir)

	aivmlog.Info("legacy monitor started (pid=%d, legacy=%s, new=%s)",
		os.Getpid(), ts.LegacyProfile, ts.NewProfile)

	ticker := time.NewTicker(m.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			count, err := m.Sessions.CountLegacy(ts.StartedAt)
			if err != nil {
				aivmlog.Warn("legacy session count error: %v", err)
				continue
			}

			if count > 0 {
				aivmlog.Debug("legacy VM '%s': %d session(s) still active", ts.LegacyProfile, count)
				continue
			}

			aivmlog.Step("All legacy sessions ended — removing legacy VM '%s'", ts.LegacyProfile)
			if err := legacyVM.Destroy(ctx); err != nil {
				aivmlog.Warn("legacy VM destroy error: %v", err)
			}
			vm.ClearTransitionState(m.StateDir)
			aivmlog.Success("Legacy VM '%s' removed — transition complete", ts.LegacyProfile)
			return nil
		}
	}
}
