package lifecycle

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
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
	// EnabledProviders is the list of all enabled agent providers (those with
	// enable: true in agents.define). Used by bootstrap to install all agents
	// and by Launch to validate --agent overrides.
	EnabledProviders []agent.Provider
	// Integrations is the complete list of integrations to evaluate during bootstrap.
	Integrations []integration.IntegrationDef
	// Confirmer handles interactive terminal I/O. Use NewTTYConfirmer() in production,
	// NewScriptedConfirmer() in tests, or &SilentConfirmer{} for non-interactive daemons.
	Confirmer Confirmer
	// GetWorkDir returns the working directory for Launch. When nil, os.Getwd is used.
	GetWorkDir func() (string, error)
	// Log is the logger used for all user-visible output. When nil, aivmlog.Default is used.
	// Inject a custom logger in tests to capture console output.
	Log *aivmlog.Logger
}

// log returns the active logger, falling back to the global default.
func (svc *LifecycleService) log() *aivmlog.Logger {
	if svc.Log != nil {
		return svc.Log
	}
	return aivmlog.Default
}

// Start starts the VM and all services, then runs bootstrap if needed.
func (svc *LifecycleService) Start(ctx context.Context) error {
	cfg := svc.Config

	svc.log().Step("Starting aivm")

	opts := buildStartOptions(svc.VM, cfg, svc.AgentDefs)

	status, err := svc.VM.Status(ctx)
	if err != nil {
		return err
	}

	if status == vm.StatusStopped && svc.shouldRecreateVM() {
		svc.log().Step("Deleting aged VM profile '%s'", svc.VM.Profile())
		if err := svc.VM.Destroy(ctx); err != nil {
			return err
		}
		status = vm.StatusNotFound
	}

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

	if cfg.T3Code.Enable {
		svc.log().Success("T3 Code mode — idle monitoring disabled")
		if err := svc.launchT3Code(ctx); err != nil {
			return err
		}
	} else {
		if err := svc.Monitor.EnsureRunning(); err != nil {
			svc.log().Warn("could not start idle monitor: %v", err)
		}
	}

	svc.log().Success("aivm is ready")
	return nil
}

// ensureBootstrapped runs the appropriate bootstrap path depending on whether
// the VM was just created.
func (svc *LifecycleService) ensureBootstrapped(ctx context.Context, wasCreated bool) error {
	if !wasCreated {
		return svc.syncBootstrap(ctx)
	}

	// Fresh VM: always run full bootstrap.
	if err := svc.fullBootstrap(ctx, svc.VM, true); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	return nil
}

// shouldRecreateVM prompts the user when the VM has exceeded its configured age threshold.
func (svc *LifecycleService) shouldRecreateVM() bool {
	cfg := svc.Config
	threshold := cfg.VM.RecreatePromptAfterDuration
	if threshold == config.DisabledDuration || threshold <= 0 {
		return false
	}
	data, err := os.ReadFile(filepath.Join(cfg.StateDir, vm.VMCreatedAtFile))
	if err != nil {
		return false
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	age := time.Since(time.Unix(epoch, 0))
	if age < threshold {
		return false
	}
	return promptVMAge(svc.log(), svc.Confirmer, svc.VM.Profile(), age, threshold) == vmAgeRecreate
}

// Stop stops the VM and all services.
func (svc *LifecycleService) Stop(ctx context.Context) error {
	svc.log().Step("Stopping aivm")
	svc.Monitor.Stop()
	if err := svc.T3Code.Stop(); err != nil {
		svc.log().Warn("T3 Code tunnel stop error: %v", err)
	}
	_ = os.Remove(filepath.Join(svc.Config.StateDir, t3codeURLFile))
	var vmErr, composeErr error
	if err := svc.VM.Stop(ctx); err != nil {
		svc.log().Warn("VM stop error: %v", err)
		vmErr = err
	}
	if err := svc.Compose.Down(ctx); err != nil {
		svc.log().Warn("compose stop error: %v", err)
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
	svc.log().Success("aivm stopped")
	return nil
}

// Destroy deletes the VM and stops all services.
func (svc *LifecycleService) Destroy(ctx context.Context) error {
	svc.Monitor.Stop()
	if err := svc.T3Code.Stop(); err != nil {
		svc.log().Warn("T3 Code tunnel stop error: %v", err)
	}
	_ = os.Remove(filepath.Join(svc.Config.StateDir, t3codeURLFile))
	var vmErr, composeErr error
	if err := svc.VM.Destroy(ctx); err != nil {
		vmErr = err
	}
	if err := svc.Compose.Down(ctx); err != nil {
		svc.log().Warn("compose destroy error: %v", err)
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
	svc.log().Success("VM destroyed")
	return nil
}

// Launch launches the configured AI agent in the VM for the current working directory.
// agentOverride selects a specific enabled agent by name; pass "" to use the default.
// T3 Code, when enabled, is started by Start() as a background service and does not
// affect this path — the agent terminal always launches regardless.
func (svc *LifecycleService) Launch(ctx context.Context, agentOverride string) error {
	cfg := svc.Config

	// Resolve which provider+def to use for this launch.
	prov := svc.Provider
	provDef := svc.AgentDefs[svc.Provider.Name()]
	if agentOverride != "" {
		found := false
		for _, p := range svc.EnabledProviders {
			if p.Name() == agentOverride {
				prov = p
				provDef = svc.AgentDefs[agentOverride]
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(svc.EnabledProviders))
			for _, p := range svc.EnabledProviders {
				names = append(names, p.Name())
			}
			sort.Strings(names)
			return fmt.Errorf("agent %q is not enabled — enabled agents: %v", agentOverride, names)
		}
	}

	getCWD := svc.GetWorkDir
	if getCWD == nil {
		getCWD = os.Getwd
	}
	hostCWD, err := getCWD()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}
	realCWD, _ := filepath.EvalSymlinks(hostCWD)
	underMount := false
	for _, m := range cfg.VM.ParsedMounts {
		realMount, _ := filepath.EvalSymlinks(m.HostPath)
		if strings.HasPrefix(realCWD, realMount) {
			underMount = true
			break
		}
	}
	if !underMount {
		return fmt.Errorf("current directory '%s' is not under any configured VM mount\naivm only works inside a mounted directory", realCWD)
	}

	if err := svc.checkVMAge(ctx); err != nil {
		return err
	}

	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	sess, err := svc.Sessions.Create(hostCWD)
	if err != nil {
		svc.log().Warn("could not create session lock: %v", err)
	} else {
		defer sess.Remove()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	done := make(chan struct{})
	defer func() {
		close(done)
		signal.Stop(sigCh)
	}()
	go func() {
		select {
		case <-sigCh:
			if sess != nil {
				sess.Remove()
			}
			os.Exit(0)
		case <-done:
		}
	}()

	vmDir := realCWD

	svc.log().Info("Host: %s", hostCWD)
	svc.log().Info("VM:   %s", vmDir)
	svc.log().Step("Launching %s in VM", prov.Description())

	providerCfg := make(map[string]any)
	if provDef.LaunchCommand != "" {
		providerCfg["launch_command"] = provDef.LaunchCommand
	}
	env := agent.LaunchEnv{
		VM:      svc.VM,
		WorkDir: vmDir,
		Config:  providerCfg,
	}

	resp, err := prov.Launch(ctx, env)
	if err != nil {
		return err
	}
	if resp != nil && resp.ExitCode != 0 {
		return fmt.Errorf("agent exited with code %d", resp.ExitCode)
	}
	return nil
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
			svc.log().Success("T3 Code is already running at http://localhost:%d", state.HostPort)
			return nil
		}
		// No longer reachable from the host — stale file. Remove it and re-launch.
		_ = os.Remove(t3codeURLPath)
	}

	if svc.T3Code.IsRunning() {
		// Same-process double-call: in-memory flag is set but state file may not
		// exist yet (race). Display port from config.
		svc.log().Success("T3 Code is already running at http://localhost:%d", containerPort)
		return nil
	}

	// For Docker VMs (NeedsPortBindingAtBoot=true), t3 serve must bind to
	// 0.0.0.0 so Docker port forwarding can reach the server from the host.
	// For Colima VMs, 127.0.0.1 is correct — the SSH tunnel connects internally.
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

	svc.log().Info("Starting T3 Code server in VM...")
	startOut, startErr := svc.VM.RunOutput(ctx, startScript, nil)
	if startErr != nil {
		return fmt.Errorf("starting t3 serve in VM: %w", startErr)
	}
	svc.log().Warn("t3 diag: %s", strings.TrimSpace(startOut))
	// Fail fast if t3 binary is missing
	if strings.Contains(startOut, "t3_diag: path=NOT_FOUND") {
		return fmt.Errorf("t3 binary not found in VM — bootstrap may have failed")
	}

	svc.log().Info("Starting T3 Code tunnel...")
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
		svc.log().Warn("Could not read T3 Code pairing info: %v", err)
		if logContents, _ := svc.VM.RunOutput(ctx, "cat /tmp/t3code.log 2>/dev/null || true", nil); strings.TrimSpace(logContents) != "" {
			svc.log().Warn("t3 serve log:\n%s", strings.TrimSpace(logContents))
		}
		return fmt.Errorf("failed to retrieve T3 Code pairing info: %w", err)
	}

	trimmedInfo := strings.TrimSpace(pairingInfo)
	if trimmedInfo == "" {
		// HTTP poll timed out or t3 serve hasn't printed pairing info yet.
		// Fail fast instead of writing a fallback URL.
		logContents, _ := svc.VM.RunOutput(ctx, "cat /tmp/t3code.log 2>/dev/null || echo '(t3code.log not found)'", nil)
		svc.log().Warn("t3 serve log:\n%s", strings.TrimSpace(logContents))
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

	fmt.Fprintln(svc.log().Out, displayInfo)
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

	if promptVMAge(svc.log(), svc.Confirmer, svc.VM.Profile(), vmAge, threshold) != vmAgeRecreate {
		return nil
	}

	sessions, err := svc.Sessions.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if len(sessions) == 0 {
		return svc.recreateCurrentVM(ctx)
	}

	svc.log().Print("\n  You have %d active session(s).", len(sessions))
	if !promptYesNo(svc.log().Out, svc.Confirmer, "  Kill all sessions and recreate now? [y/N] ") {
		svc.log().Print("\n  VM recreation cancelled.")
		return nil
	}

	svc.log().Step("Killing %d active session(s)...", len(sessions))
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
	svc.log().Step("Stopping current VM...")
	if err := svc.Stop(ctx); err != nil {
		svc.log().Warn("Stop error (continuing): %v", err)
	}

	clearBootstrapState(svc.Config.StateDir)

	svc.log().Step("Destroying VM...")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	svc.log().Step("Starting fresh VM...")
	return svc.Start(ctx)
}

// Bootstrap runs the bootstrap process. When force is true all plugins are re-run.
func (svc *LifecycleService) Bootstrap(ctx context.Context, onlyPlugin string, force bool) error {
	status, err := svc.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	if onlyPlugin != "" {
		eng := svc.newBootstrapEngine(svc.VM, []string{onlyPlugin})
		if err := eng.Run(ctx, force); err != nil {
			return err
		}
		return svc.recordBootstrapState()
	}

	if force {
		return svc.fullBootstrap(ctx, svc.VM, true)
	}

	return svc.syncBootstrap(ctx)
}

// Recreate destroys the current VM and starts a fresh one, re-running bootstrap.
// With force=true, active sessions are killed without prompting.
func (svc *LifecycleService) Recreate(ctx context.Context, force bool) error {
	sessions, err := svc.Sessions.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}
	if len(sessions) > 0 {
		svc.log().Warn("%d active session(s) detected.", len(sessions))
	}

	if force {
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.log().Info("Sent SIGTERM to %d session(s).", len(killed))
		}
	} else {
		prompt := "\n  Proceed with VM recreation? [y/N] "
		if len(sessions) > 0 {
			prompt = "\n  Kill all sessions and recreate the VM? [y/N] "
		}
		if !promptYesNo(svc.log().Out, svc.Confirmer, prompt) {
			svc.log().Print("\n  VM recreation cancelled.")
			return nil
		}
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.log().Info("Sent SIGTERM to %d session(s).", len(killed))
		}
	}

	// Stop the idle monitor before destroying the VM. If Destroy fails the
	// monitor stays stopped; Start will restart it via its normal launch path.
	svc.Monitor.Stop()

	clearBootstrapState(svc.Config.StateDir)

	svc.log().Step("Destroying existing VM")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	return svc.Start(ctx)
}
