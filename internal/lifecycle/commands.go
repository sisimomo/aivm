package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
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
	fmt.Fprintf(out, "  │  VM (%s): %s %s\n", svc.VM.Profile(), vmIcon, status)

	// Compose services section — only shown when compose_file is configured.
	if cfg.ComposeFile != "" {
		hm := svc.Compose.HealthMap(ctx)
		serviceNames := make([]string, 0, len(hm))
		for n := range hm {
			serviceNames = append(serviceNames, n)
		}
		sort.Strings(serviceNames)
		for _, n := range serviceNames {
			icon := "❌"
			if hm[n] {
				icon = "✅"
			}
			fmt.Fprintf(out, "  │  Compose %-12s %s\n", n+":", icon)
		}
	}

	if cfg.T3Code.Enable {
		t3Icon := "❌"
		// Resolve the host port and display URL in one pass from the state file.
		// Probe from the host: verifies both t3 serve and port-forwarding are working.
		state := readT3CodeState(cfg.StateDir, cfg.T3Code.Port)
		if t3CodeIsAlive(state.HostPort) {
			t3Icon = "✅"
		}
		fmt.Fprintf(out, "  │  T3 Code:           %s %s\n", t3Icon, state.DisplayURL)
		fmt.Fprintf(out, "  │  Idle monitor:      — (disabled in T3 Code mode)\n")
	} else {
		monitorPID := filepath.Join(cfg.StateDir, "idle-monitor.pid")
		monitorIcon := "❌"
		if _, err := os.Stat(monitorPID); err == nil {
			monitorIcon = "✅"
		}
		fmt.Fprintf(out, "  │  Idle monitor:      %s\n", monitorIcon)
	}

	sessions, _ := svc.Sessions.List()
	fmt.Fprintf(out, "  │  Active sessions:   %d\n", len(sessions))

	if !cfg.T3Code.Enable {
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
	}

	fmt.Fprintln(out, "  └───────────────────────────────────────────────┘")
	fmt.Fprintln(out)
	return nil
}

// SSH ensures the VM is running (starting and bootstrapping if needed) then
// opens an interactive shell, creating a session lock for the duration.
func (svc *LifecycleService) SSH(ctx context.Context) error {
	if err := svc.Start(ctx); err != nil {
		return err
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

// Logs streams logs for the given service. Built-in services are
// "monitor" (or "idle-monitor"), "bootstrap", and "vm". An empty service
// name (or omitting the argument) streams all compose service logs.
func (svc *LifecycleService) Logs(service string) error {
	stateDir := svc.Config.StateDir
	switch service {
	case "", "compose":
		return svc.Compose.Logs()
	case "monitor", "idle-monitor":
		return tailFile(filepath.Join(stateDir, "logs", "idle-monitor.log"))
	case "bootstrap":
		return tailFile(filepath.Join(stateDir, "logs", "bootstrap.log"))
	case "vm":
		backend := svc.Config.VM.Backend
		if backend == "" {
			backend = "colima" // default backend
		}
		logFile := fmt.Sprintf("%s.log", backend)
		return tailFile(filepath.Join(stateDir, "logs", logFile))
	default:
		return fmt.Errorf("unknown service %q — valid services: monitor, bootstrap, vm (or omit for compose logs)", service)
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

// Copy transfers a file or directory between the host and the VM.
//
// Paths inside the VM must be prefixed with "vm:" (e.g. "vm:/home/user/file").
// One of src or dst must use the vm: prefix; the other is a plain host path.
//
// When recursive is true, directories are copied recursively (equivalent to
// scp -r / docker cp on a directory). When force is false and the destination
// already exists the command returns an error instead of overwriting.
func (svc *LifecycleService) Copy(ctx context.Context, src, dst string, recursive, force bool) error {
	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	const vmPrefix = "vm:"
	srcIsVM := strings.HasPrefix(src, vmPrefix)
	dstIsVM := strings.HasPrefix(dst, vmPrefix)

	switch {
	case srcIsVM && dstIsVM:
		return fmt.Errorf("both source and destination cannot be VM paths")
	case !srcIsVM && !dstIsVM:
		return fmt.Errorf("one of source or destination must be a VM path (use 'vm:' prefix, e.g. vm:/home/user/file)")
	}

	if srcIsVM {
		// VM → host
		vmPath := strings.TrimPrefix(src, vmPrefix)
		localPath := dst
		if !force {
			if _, statErr := os.Stat(localPath); statErr == nil {
				return fmt.Errorf("destination %q already exists — use -f/--force to overwrite", localPath)
			}
		}
		return svc.VM.CopyFrom(ctx, vmPath, localPath, recursive)
	}

	// host → VM
	vmPath := strings.TrimPrefix(dst, vmPrefix)
	localPath := src
	if !force {
		out, runErr := svc.VM.RunOutput(ctx, "test -e "+vm.ShellEscape(vmPath)+" && echo exists || echo notfound", nil)
		if runErr == nil && strings.TrimSpace(out) == "exists" {
			return fmt.Errorf("destination %q already exists in VM — use -f/--force to overwrite", vmPath)
		}
	}
	return svc.VM.CopyTo(ctx, localPath, vmPath, recursive)
}
