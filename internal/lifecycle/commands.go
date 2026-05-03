package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/sisimomo/aivm/internal/vm"
)

// Status displays VM, MCP, session, and monitor status to stdout.
func (svc *LifecycleService) Status(ctx context.Context) error {
	cfg := svc.Config
	out := svc.log().Out

	fmt.Fprintln(out)
	fmt.Fprintln(out, "  ┌─ aivm status ─────────────────────────────────┐")

	status, _ := svc.VM.Status(ctx)
	vmIcon := "❌"
	if status == vm.StatusRunning {
		vmIcon = "✅"
	}
	fmt.Fprintf(out, "  │  VM (%s): %s %s\n", cfg.VM.ColimaProfile, vmIcon, status)

	imgMgr := svc.imageManager()
	baseImg := imgMgr.LoadBaseImage()
	if baseImg != nil {
		fmt.Fprintf(out, "  │  Base image:        id=%s (%s)\n",
			baseImg.ID, baseImg.CreatedAt.Local().Format("2006-01-02 15:04 MST"))
		vmRef := imgMgr.GetVMImageRef()
		if vmRef == "" {
			fmt.Fprintf(out, "  │  VM image ref:      (unknown)\n")
		} else {
			fmt.Fprintf(out, "  │  VM image ref:      id=%s\n", vmRef)
		}
	} else {
		fmt.Fprintf(out, "  │  Base image:        (none — run 'aivm start' to create)\n")
	}

	mcpIcon := "❌"
	if svc.MCP.IsHealthy(ctx) {
		mcpIcon = "✅"
	}
	fmt.Fprintf(out, "  │  MCPJungle:         %s port %d\n", mcpIcon, cfg.MCP.Port)

	monitorPID := filepath.Join(cfg.StateDir, "idle-monitor.pid")
	monitorIcon := "❌"
	if _, err := os.Stat(monitorPID); err == nil {
		monitorIcon = "✅"
	}
	fmt.Fprintf(out, "  │  Idle monitor:      %s\n", monitorIcon)

	sessions, _ := svc.Sessions.List()
	fmt.Fprintf(out, "  │  Active sessions:   %d\n", len(sessions))

	if len(sessions) == 0 {
		last := svc.Sessions.ReadLastActive()
		idle := time.Since(last).Round(time.Second)

		switch status {
		case vm.StatusRunning:
			remaining := cfg.Idle.StopTimeout - idle
			if remaining > 0 {
				fmt.Fprintf(out, "  │  Idle suspend:      in %s\n", remaining)
			} else {
				fmt.Fprintf(out, "  │  Idle suspend:      ⚠️  imminent\n")
			}
		case vm.StatusStopped:
			stoppedAt := svc.Sessions.ReadVMStoppedAt()
			if !stoppedAt.IsZero() {
				elapsed := time.Since(stoppedAt).Round(time.Second)
				remaining := cfg.Idle.DeleteTimeout - elapsed
				if remaining > 0 {
					fmt.Fprintf(out, "  │  VM deletion:       in %s\n", remaining)
				} else {
					fmt.Fprintf(out, "  │  VM deletion:       ⚠️  imminent\n")
				}
			}
		}
	} else {
		fmt.Fprintf(out, "  │  Idle suspend:      ─ sessions active\n")
	}

	fmt.Fprintln(out, "  └───────────────────────────────────────────────┘")
	fmt.Fprintln(out)
	return nil
}

// SSH opens an interactive shell in the VM, creating a session lock for the duration.
func (svc *LifecycleService) SSH(ctx context.Context) error {
	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	workDir, _ := os.Getwd()
	sess, err := svc.Sessions.Create(workDir)
	if err != nil {
		svc.log().Warn("could not create session lock: %v", err)
	} else {
		defer sess.Remove()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		if sess != nil {
			sess.Remove()
		}
		os.Exit(0)
	}()

	return svc.VM.SSH(ctx)
}

// Logs streams logs for the given service. Service may be one of:
// "mcpjungle", "monitor", "idle-monitor", "bootstrap", "colima".
func (svc *LifecycleService) Logs(service string) error {
	stateDir := svc.Config.StateDir
	switch service {
	case "mcpjungle":
		return svc.MCP.Logs()
	case "monitor", "idle-monitor":
		return tailFile(filepath.Join(stateDir, "logs", "idle-monitor.log"))
	case "bootstrap":
		return tailFile(filepath.Join(stateDir, "logs", "bootstrap.log"))
	case "colima":
		return tailFile(filepath.Join(stateDir, "logs", "colima.log"))
	default:
		return fmt.Errorf("unknown service: %s\nAvailable: mcpjungle | monitor | bootstrap | colima", service)
	}
}

func tailFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", path)
	}
	cmd := exec.Command("tail", "-f", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ListPlugins prints all known plugins and their status (enabled or disabled).
func (svc *LifecycleService) ListPlugins() error {
	out := svc.log().Out
	all := svc.Registry.All()
	fmt.Fprintf(out, "\n  %-16s %-40s %s\n", "NAME", "DESCRIPTION", "DEPENDS ON")
	fmt.Fprintf(out, "  %-16s %-40s %s\n", "────────────────", "────────────────────────────────────────", "──────────")
	ordered, _ := svc.Registry.Resolve(svc.Config.Plugins.Enabled)
	shown := make(map[string]bool)
	for _, p := range ordered {
		shown[p.Name()] = true
		fmt.Fprintf(out, "  %-16s %-40s %v\n", p.Name(), p.Description(), p.Dependencies())
	}
	for name, p := range all {
		if !shown[name] {
			fmt.Fprintf(out, "  %-16s %-40s %v  (disabled)\n", p.Name(), p.Description(), p.Dependencies())
		}
	}
	fmt.Fprintln(out)
	return nil
}

// RunMonitor runs the idle monitor daemon in the current process.
func (svc *LifecycleService) RunMonitor(ctx context.Context) error {
	return svc.Monitor.Run(ctx)
}
