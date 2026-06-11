package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/compose"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/monitor"
	"github.com/sisimomo/aivm/internal/plugin"
	"github.com/sisimomo/aivm/internal/session"
	"github.com/sisimomo/aivm/internal/t3code"
	"github.com/sisimomo/aivm/internal/vm"
)

// LifecycleService owns all orchestration logic for the aivm VM lifecycle.
// CLI commands are thin adapters that call into this service.
type LifecycleService struct {
	Config   *config.Config
	VM       vm.VM
	Compose  compose.ComposeManager
	T3Code   t3code.Manager
	Sessions *session.Store
	Monitor  *monitor.IdleMonitor
	Registry *plugin.Registry
	Agents   *agent.Registry
	// AgentDefs is the effective set of agent definitions for ALL enabled agents
	// (built-in defaults merged with user overrides). Keys are agent names.
	// Used by Launch to pass runtime config to the provider and by bootstrap
	// to install every enabled agent in the VM.
	AgentDefs map[string]agent.Def
	// PluginDefs is the effective set of all plugin definitions after merging
	// built-in defaults, agent definitions, and user overrides. Used for config
	// hash computation (change detection).
	PluginDefs map[string]plugin.PluginDef
	// Provider is the default AI agent provider (from agents.default).
	// Used for hash/state comparison and as the launch target when no --agent
	// override is given.
	Provider agent.Provider
	// EnabledProviders is the list of all enabled agent providers (those listed in
	// agents.enabled). Used by bootstrap to install all agents
	// and by Launch to validate --agent overrides.
	EnabledProviders []agent.Provider
	// Integrations is the complete list of integrations to evaluate during bootstrap.
	Integrations []integration.IntegrationDef
	// Confirmer handles interactive terminal I/O. Use NewTTYConfirmer() in production,
	// NewScriptedConfirmer() in tests, or &SilentConfirmer{} for non-interactive daemons.
	Confirmer Confirmer
	// GetWorkDir returns the working directory for Launch. When nil, os.Getwd is used.
	GetWorkDir func() (string, error)
	// Log overrides slog.Default() in tests. When nil, slog.Default() is used.
	Log *slog.Logger
}

func (svc *LifecycleService) logger() *slog.Logger {
	if svc.Log != nil {
		return svc.Log
	}
	return slog.Default()
}

// Start starts the VM and all services, then runs bootstrap if needed.
func (svc *LifecycleService) Start(ctx context.Context) error {
	cfg := svc.Config

	svc.logger().Info("Starting aivm")

	status, err := svc.VM.Status(ctx)
	if err != nil {
		return err
	}

	action, err := svc.decideStartAction(ctx, status)
	if err != nil {
		return err
	}

	switch action {
	case ActionFullBootstrap:
		if err := svc.fullBootstrap(ctx); err != nil {
			return err
		}
	case ActionFastRecreate:
		if err := svc.fastRecreate(ctx); err != nil {
			return err
		}
	case ActionPromptBootstrapRefresh, ActionPromptVMAge, ActionPromptCombined, ActionPromptRuntimeChange, ActionPromptConfigChange:
		if err := svc.handleRecreationPrompt(ctx, action, status); err != nil {
			return err
		}
	default:
		if err := svc.resumeOrStartVM(ctx, status); err != nil {
			return err
		}
	}

	if cfg.T3Code.Enable {
		svc.logger().Info("T3 Code mode — idle monitoring disabled")
		if err := svc.launchT3Code(ctx); err != nil {
			return err
		}
	} else {
		if err := svc.Monitor.EnsureRunning(); err != nil {
			svc.logger().Warn(fmt.Sprintf("could not start idle monitor: %v", err))
		}
	}

	svc.logger().Info("aivm is ready")
	return nil
}

func (svc *LifecycleService) resumeOrStartVM(ctx context.Context, status vm.Status) error {
	cfg := svc.Config
	opts := buildStartOptions(svc.VM, cfg, svc.AgentDefs)
	wasCreated := status == vm.StatusNotFound
	needsStart := status != vm.StatusRunning

	ensureAgentPersistDirs(cfg, svc.AgentDefs)

	if err := svc.VM.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}

	if wasCreated {
		vm.RecordVMCreation(cfg.StateDir)
	}

	if needsStart {
		if err := svc.VM.WaitReady(ctx, 60*time.Second); err != nil {
			return err
		}
		svc.Sessions.ClearVMStoppedAt()
	}

	if err := svc.ensureBootstrapped(ctx, wasCreated); err != nil {
		return err
	}

	if needsStart {
		if err := svc.Compose.Up(ctx); err != nil {
			return fmt.Errorf("starting compose services: %w", err)
		}
	}

	return nil
}

// ensureBootstrapped runs the appropriate bootstrap path depending on whether
// the VM was just created.
func (svc *LifecycleService) ensureBootstrapped(ctx context.Context, wasCreated bool) error {
	if !wasCreated {
		return svc.syncBootstrap(ctx)
	}

	// Fresh VM: always run bootstrap.
	if err := svc.bootstrap(ctx, svc.VM); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return nil
}

// Stop stops the VM and all services.
func (svc *LifecycleService) Stop(ctx context.Context) error {
	svc.logger().Info("Stopping aivm")
	svc.Monitor.Stop()
	if err := svc.T3Code.Stop(); err != nil {
		svc.logger().Warn(fmt.Sprintf("T3 Code tunnel stop error: %v", err))
	}
	_ = os.Remove(filepath.Join(svc.Config.StateDir, t3codeURLFile))
	var vmErr, composeErr error
	if err := svc.VM.Stop(ctx); err != nil {
		svc.logger().Warn(fmt.Sprintf("VM stop error: %v", err))
		vmErr = err
	}
	if err := svc.Compose.Down(ctx); err != nil {
		svc.logger().Warn(fmt.Sprintf("compose stop error: %v", err))
		composeErr = err
	}
	if vmErr != nil || composeErr != nil {
		if vmErr != nil && composeErr != nil {
			return fmt.Errorf("VM stop: %w; compose stop: %v", vmErr, composeErr)
		}
		if vmErr != nil {
			return fmt.Errorf("VM stop: %w", vmErr)
		}
		return fmt.Errorf("compose stop: %w", composeErr)
	}
	svc.logger().Info("aivm stopped")
	return nil
}

// Destroy deletes the VM and stops all services.
func (svc *LifecycleService) Destroy(ctx context.Context) error {
	svc.Monitor.Stop()
	if err := svc.T3Code.Stop(); err != nil {
		svc.logger().Warn(fmt.Sprintf("T3 Code tunnel stop error: %v", err))
	}
	_ = os.Remove(filepath.Join(svc.Config.StateDir, t3codeURLFile))
	var vmErr, composeErr error
	if err := svc.VM.Destroy(ctx); err != nil {
		vmErr = err
	}
	if err := svc.Compose.Down(ctx); err != nil {
		svc.logger().Warn(fmt.Sprintf("compose destroy error: %v", err))
		composeErr = err
	}
	if vmErr != nil || composeErr != nil {
		if vmErr != nil && composeErr != nil {
			return fmt.Errorf("VM destroy: %w; compose destroy: %v", vmErr, composeErr)
		}
		if vmErr != nil {
			return fmt.Errorf("VM destroy: %w", vmErr)
		}
		return fmt.Errorf("compose destroy: %w", composeErr)
	}
	svc.logger().Info("VM destroyed")
	return nil
}

// Launch launches the configured AI agent in the VM for the current working directory.
// agentOverride selects a specific enabled agent by name; pass "" to use the default.
// T3 Code, when enabled, is started by Start() as a background service and does not
// affect this path — the agent terminal always launches regardless.
func (svc *LifecycleService) Launch(ctx context.Context, agentOverride string) error {
	cfg := svc.Config

	s, err := svc.prepareAgentSession(ctx, agentOverride)
	if err != nil {
		return err
	}
	defer s.cleanup()

	svc.logger().Debug(fmt.Sprintf("Host: %s", s.hostCWD))
	svc.logger().Debug(fmt.Sprintf("VM:   %s", s.vmDir))
	svc.logger().Info(fmt.Sprintf("Launching %s in VM", s.prov.Description()))

	env := agent.LaunchEnv{
		VM:         svc.VM,
		WorkDir:    s.vmDir,
		CLICommand: s.provDef.CLICommand,
		LaunchArgs: s.provDef.LaunchArgs,
		Env:        cfg.VM.ResolvedSessionEnv(),
	}

	resp, err := s.prov.Launch(s.ctx, env)
	return agentExitError(resp, err)
}

// AgentRun executes the agent CLI in the VM with user-supplied arguments (aivm agent -- …).
func (svc *LifecycleService) AgentRun(ctx context.Context, agentOverride string, args []string) error {
	cfg := svc.Config

	s, err := svc.prepareAgentSession(ctx, agentOverride)
	if err != nil {
		return err
	}
	defer s.cleanup()

	svc.logger().Debug(fmt.Sprintf("Host: %s", s.hostCWD))
	svc.logger().Debug(fmt.Sprintf("VM:   %s", s.vmDir))
	svc.logger().Info(fmt.Sprintf("Running %s in VM", s.prov.Description()))

	env := agent.RunEnv{
		VM:         svc.VM,
		WorkDir:    s.vmDir,
		CLICommand: s.provDef.CLICommand,
		Args:       args,
		Env:        cfg.VM.ResolvedSessionEnv(),
	}

	resp, err := s.prov.Run(s.ctx, env)
	return agentExitError(resp, err)
}

// launchT3Code starts t3 serve inside the VM and port-forwards the configured
// port to the host. It returns immediately after starting the tunnel — no
// session lock is created and no terminal is blocked.
func (svc *LifecycleService) launchT3Code(ctx context.Context) error {
	cfg := svc.Config

	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	// Determine the port t3 serve should listen on inside the container.
	// When cfg.T3Code.Port == 0, Docker auto-assigns a host port mapped to the
	// default T3 Code container port (3773). Use 3773 so t3 serve actually binds
	// on the port that Docker is forwarding — port 0 would result in an
	// OS-assigned random port that Docker cannot reach.
	containerPort := cfg.T3Code.Port
	if containerPort == 0 {
		containerPort = 3773
	}

	// Check the persistent state file first. Manager.IsRunning() is in-memory
	// only and loses state between process invocations (e.g. `aivm start` exits
	// and then `aivm` bare calls Start() again in a new process). The t3code-url
	// file is written after a successful launch and removed by Stop()/Destroy(),
	// so its presence means a previous process launched t3 serve successfully.
	t3codeURLPath := filepath.Join(cfg.StateDir, t3codeURLFile)
	if _, statErr := os.Stat(t3codeURLPath); statErr == nil {
		// Parse the host port from the persisted pairing URL (handles Docker
		// auto-assigned ports) and probe from the host to confirm reachability.
		state := readT3CodeState(cfg.StateDir, containerPort)
		if t3CodeIsAlive(state.HostPort) {
			svc.logger().Info(fmt.Sprintf("T3 Code is already running at http://localhost:%d", state.HostPort))
			return nil
		}
		// No longer reachable from the host — stale file. Remove it and re-launch.
		_ = os.Remove(t3codeURLPath)
	}

	if svc.T3Code.IsRunning() {
		// Same-process double-call: in-memory flag is set but state file may not
		// exist yet (race). Display port from config.
		svc.logger().Info(fmt.Sprintf("T3 Code is already running at http://localhost:%d", containerPort))
		return nil
	}

	// For Docker VMs (NeedsPortBindingAtBoot=true), t3 serve must bind to
	// 0.0.0.0 so Docker port forwarding can reach the server from the host.
	// For Lima VMs, 127.0.0.1 is correct — the SSH tunnel connects internally.
	bindHost := "127.0.0.1"
	if svc.VM.NeedsPortBindingAtBoot() {
		bindHost = "0.0.0.0"
	}

	// Daemonize t3 serve inside the VM. nohup + & ensures it survives the SSH
	// session closing. mise shims are on PATH via /etc/profile.d/aivm-path.sh
	// which is sourced by every login shell (all VM.Run calls use bash -lc).
	startScript := fmt.Sprintf(`
t3_path=$(command -v t3 2>/dev/null || echo "NOT_FOUND")
echo "t3_diag: path=$t3_path"
if [ "$t3_path" = "NOT_FOUND" ]; then
    echo "t3_diag: PATH=$PATH"
else
    t3_ver=$(t3 --version 2>&1 | head -1)
    echo "t3_diag: version=$t3_ver"
    nohup t3 serve --host %s --port %d > /tmp/t3code.log 2>&1 &
    echo "t3_diag: serve_pid=$!"
fi
`, bindHost, containerPort)

	svc.logger().Debug("Starting T3 Code server in VM...")
	startOut, startErr := svc.VM.RunOutput(ctx, startScript, nil)
	if startErr != nil {
		return fmt.Errorf("starting t3 serve in VM: %w", startErr)
	}
	svc.logger().Warn(fmt.Sprintf("t3 diag: %s", strings.TrimSpace(startOut)))
	// Fail fast if t3 binary is missing
	if strings.Contains(startOut, "t3_diag: path=NOT_FOUND") {
		return fmt.Errorf("t3 binary not found in VM — bootstrap may have failed")
	}

	svc.logger().Debug("Starting T3 Code tunnel...")
	if err := svc.T3Code.Launch(ctx, containerPort); err != nil {
		return fmt.Errorf("starting T3 Code tunnel: %w", err)
	}

	// Poll via HTTP from inside the VM for readiness — more robust than
	// grepping the log for a specific string (avoids log-format fragility).
	// After the server responds, display any pairing info from the log.
	// Poll for HTTP readiness. --max-time 3 prevents the first few curl attempts
	// from hanging indefinitely when t3 serve has accepted the TCP connection but
	// hasn't yet sent back a response (happens during its startup window).
	pairingScript := fmt.Sprintf(`
for i in $(seq 1 60); do
    if curl -sf --max-time 3 http://localhost:%d/ >/dev/null 2>&1; then
        break
    fi
    sleep 0.5
done
sed -n '/T3 Code server is ready/,$p' /tmp/t3code.log 2>/dev/null || true
`, containerPort)

	pairingInfo, err := svc.VM.RunOutput(ctx, pairingScript, nil)
	if err != nil {
		svc.logger().Warn(fmt.Sprintf("Could not read T3 Code pairing info: %v", err))
		if logContents, _ := svc.VM.RunOutput(ctx, "cat /tmp/t3code.log 2>/dev/null || true", nil); strings.TrimSpace(logContents) != "" {
			svc.logger().Warn(fmt.Sprintf("t3 serve log:\n%s", strings.TrimSpace(logContents)))
		}
		return fmt.Errorf("failed to retrieve T3 Code pairing info: %w", err)
	}

	trimmedInfo := strings.TrimSpace(pairingInfo)
	if trimmedInfo == "" {
		// HTTP poll timed out or t3 serve hasn't printed pairing info yet.
		// Fail fast instead of writing a fallback URL.
		logContents, _ := svc.VM.RunOutput(ctx, "cat /tmp/t3code.log 2>/dev/null || echo '(t3code.log not found)'", nil)
		svc.logger().Warn(fmt.Sprintf("t3 serve log:\n%s", strings.TrimSpace(logContents)))
		return fmt.Errorf("T3 Code server did not respond or print pairing info within timeout")
	}

	// Rewrite any VM-internal IP address to localhost so every URL the user
	// sees is consistent and actually reachable from the host. t3 serve may
	// advertise its container IP (e.g. 172.17.0.x) rather than 127.0.0.1,
	// so we replace any IPv4 address rather than only 127.0.0.1.
	displayInfo := rewriteIPsToLocalhost(trimmedInfo)

	// Derive the host-side port for the pairing URL. For Docker with port 0
	// (auto-assign), the host port differs from containerPort and must be
	// queried from Docker. For all other cases host port == containerPort.
	hostPort := containerPort
	if cfg.T3Code.Port == 0 && svc.VM.NeedsPortBindingAtBoot() {
		if p, err := svc.VM.GetPublishedPort(containerPort); err == nil && p > 0 {
			hostPort = p
		}
	}

	pairingURL := parsePairingURL(pairingInfo, hostPort)
	_ = os.WriteFile(filepath.Join(cfg.StateDir, t3codeURLFile), []byte(pairingURL), 0600)

	fmt.Fprintln(aivmlog.TerminalOut(), displayInfo)
	return nil
}

// parsePairingURL extracts the "Pairing URL:" line from t3 serve startup output,
// rewrites any VM-internal IP address to localhost (exposed by the SSH tunnel or
// Docker port forwarding), and rewrites the port to hostPort (important when
// Docker auto-assigns a host port that differs from the container port).
// Falls back to a bare URL if no pairing line is found.
func parsePairingURL(output string, hostPort int) string {
	for _, line := range strings.Split(output, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "Pairing URL:"); ok {
			u := rewriteIPsToLocalhost(strings.TrimSpace(after))
			return rewriteURLPort(u, hostPort)
		}
	}
	return fmt.Sprintf("http://localhost:%d", hostPort)
}

// rewriteURLPort replaces the port in rawURL with newPort. If the URL cannot
// be parsed or has no explicit port, rawURL is returned unchanged.
func rewriteURLPort(rawURL string, newPort int) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host, _, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		// No port in host — nothing to rewrite.
		return rawURL
	}
	parsed.Host = net.JoinHostPort(host, strconv.Itoa(newPort))
	return parsed.String()
}

// rewriteIPsToLocalhost replaces all IPv4 addresses in s with "localhost".
// t3 serve advertises its container/VM IP (e.g. 172.17.0.x) rather than
// 127.0.0.1, so we normalise all IPs so URLs are reachable from the host.
var ipRe = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

func rewriteIPsToLocalhost(s string) string {
	return ipRe.ReplaceAllString(s, "localhost")
}

// checkVMAge prompts the user when the VM is older than the configured
// recreate_prompt_after threshold. It may recreate the current VM after
// confirming with the user.
func (svc *LifecycleService) checkVMAge(ctx context.Context) error {
	cfg := svc.Config

	if !svc.Confirmer.IsInteractive() {
		return nil
	}
	if vmCreatedRecently(cfg.StateDir) {
		return nil
	}

	threshold := cfg.VM.RecreatePromptAfterDuration
	if threshold == config.DisabledDuration || threshold <= 0 {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(cfg.StateDir, vm.VMCreatedAtFile))
	if err != nil {
		return nil
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return nil
	}
	vmAge := time.Since(time.Unix(epoch, 0))
	if vmAge < threshold {
		return nil
	}

	if promptVMAge(svc.Confirmer, svc.VM.Profile(), vmAge, threshold) != vmAgeRecreate {
		return nil
	}

	sessions, err := svc.Sessions.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		return svc.recreateCurrentVM(ctx)
	}

	fmt.Fprintf(aivmlog.TerminalOut(), "\n  You have %d active session(s).\n", len(sessions))
	if !PromptYesNo(aivmlog.TerminalOut(), svc.Confirmer, "  Kill all sessions and recreate now? [y/N] ", false) {
		fmt.Fprintln(aivmlog.TerminalOut(), "\n  VM recreation cancelled.")
		return nil
	}

	svc.logger().Info(fmt.Sprintf("Killing %d active session(s)...", len(sessions)))
	for _, s := range sessions {
		proc, err := os.FindProcess(s.PID)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
		s.Remove()
	}
	return svc.recreateCurrentVM(ctx)
}

// recreateCurrentVM destroys the current VM and starts a fresh one.
func (svc *LifecycleService) recreateCurrentVM(ctx context.Context) error {
	svc.logger().Info("Stopping current VM...")
	if err := svc.Stop(ctx); err != nil {
		svc.logger().Warn(fmt.Sprintf("Stop error (continuing): %v", err))
	}

	clearBootstrapState(svc.Config.StateDir)

	svc.logger().Info("Destroying VM...")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	svc.logger().Info("Starting fresh VM...")
	return svc.Start(ctx)
}

// Recreate destroys the current VM and starts a fresh one, re-running bootstrap.
// With force=true, active sessions are killed without prompting.
func (svc *LifecycleService) Recreate(ctx context.Context, force bool) error {
	sessions, err := svc.Sessions.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if len(sessions) > 0 {
		svc.logger().Warn(fmt.Sprintf("%d active session(s) detected.", len(sessions)))
	}

	if force {
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.logger().Info(fmt.Sprintf("Sent SIGTERM to %d session(s).", len(killed)))
		}
	} else {
		prompt := "\n  Proceed with VM recreation? [y/N] "
		if len(sessions) > 0 {
			prompt = "\n  Kill all sessions and recreate the VM? [y/N] "
		}
		if !PromptYesNo(aivmlog.TerminalOut(), svc.Confirmer, prompt, false) {
			fmt.Fprintln(aivmlog.TerminalOut(), "\n  VM recreation cancelled.")
			return nil
		}
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.logger().Info(fmt.Sprintf("Sent SIGTERM to %d session(s).", len(killed)))
		}
	}

	// Stop the idle monitor before destroying the VM. If Destroy fails the
	// monitor stays stopped; Start will restart it via its normal launch path.
	svc.Monitor.Stop()

	clearBootstrapState(svc.Config.StateDir)

	svc.logger().Info("Destroying existing VM")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	return svc.Start(ctx)
}
