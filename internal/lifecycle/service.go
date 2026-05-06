package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sisimomo/aivm/internal/agent"
	"github.com/sisimomo/aivm/internal/config"
	"github.com/sisimomo/aivm/internal/integration"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/mcp"
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
	MCP      mcp.MCPManager
	T3Code   t3code.Manager
	Sessions *session.Store
	Monitor  *monitor.IdleMonitor
	Registry *plugin.Registry
	Agents   *agent.Registry
	// AgentDefs is the effective set of agent definitions (built-in defaults
	// merged with user overrides). Used by Launch to pass runtime config to the provider.
	AgentDefs map[string]agent.Def
	// PluginDefs is the effective set of all plugin definitions after merging
	// built-in defaults, agent definitions, and user overrides. Used for config
	// hash computation (change detection).
	PluginDefs map[string]plugin.PluginDef
	// Provider is the active AI agent provider selected from the config.
	Provider agent.Provider
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

// imageManager returns an ImageManager scoped to the service's VM and state dir.
func (svc *LifecycleService) imageManager() *vm.ImageManager {
	return vm.NewImageManager(svc.VM, svc.Config.StateDir)
}

// activeAgentDef returns the effective agent definition for the active provider.
func (svc *LifecycleService) activeAgentDef() agent.Def {
	return svc.AgentDefs[svc.Provider.Name()]
}

// Start starts the VM and all services, then runs bootstrap if needed.
func (svc *LifecycleService) Start(ctx context.Context) error {
	cfg := svc.Config

	svc.log().Step("Starting aivm")

	svc.log().Info("Ensuring MCPJungle is running...")
	if err := svc.MCP.Start(ctx); err != nil {
		return fmt.Errorf("starting MCPJungle: %w", err)
	}

	opts := buildStartOptions(cfg, svc.activeAgentDef())

	status, err := svc.VM.Status(ctx)
	if err != nil {
		return err
	}

	if status == vm.StatusStopped && svc.shouldRecreateVM() {
		svc.log().Step("Deleting aged VM profile '%s'", cfg.VM.ColimaProfile)
		if err := svc.VM.Destroy(ctx); err != nil {
			return err
		}
		status = vm.StatusNotFound
	}

	wasCreated := status == vm.StatusNotFound
	needsStart := status != vm.StatusRunning

	ensureAgentPersistDirs(cfg, svc.activeAgentDef())

	if err := svc.VM.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting VM: %w", err)
	}

	imgMgr := svc.imageManager()
	if wasCreated {
		imgMgr.RecordCreation()
	}

	if needsStart {
		if err := svc.VM.WaitReady(ctx, 60*time.Second); err != nil {
			return err
		}
		svc.Sessions.ClearVMStoppedAt()
	}

	if err := svc.ensureBootstrapped(ctx, wasCreated, imgMgr); err != nil {
		return err
	}

	if cfg.T3Code.Enable {
		svc.log().Info("T3 Code mode — idle monitoring disabled")
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
// the VM was just created and whether a base image exists.
func (svc *LifecycleService) ensureBootstrapped(ctx context.Context, wasCreated bool, imgMgr *vm.ImageManager) error {
	if !wasCreated {
		return svc.syncBootstrap(ctx)
	}

	if imgMgr.TryRestoreBaseImage(ctx) {
		clearBootstrapState(svc.Config.StateDir)
		return svc.syncBootstrap(ctx)
	}

	// Fresh VM, no base image: full bootstrap then save a new base image.
	if err := svc.fullBootstrap(ctx, svc.VM, true); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		svc.log().Warn("could not save base image (non-fatal): %v", err)
	} else {
		imgMgr.RecordVMImageRef(img.ID)
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
	data, err := os.ReadFile(filepath.Join(cfg.StateDir, "vm-created-at"))
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
	return promptVMAge(svc.log(), svc.Confirmer, cfg.VM.ColimaProfile, age, threshold) == vmAgeRecreate
}

// Stop stops the VM and all services.
func (svc *LifecycleService) Stop(ctx context.Context) error {
	svc.log().Step("Stopping aivm")
	svc.Monitor.Stop()
	if err := svc.T3Code.Stop(); err != nil {
		svc.log().Warn("T3 Code tunnel stop error: %v", err)
	}
	_ = os.Remove(filepath.Join(svc.Config.StateDir, "t3code-url"))
	if err := svc.VM.Stop(ctx); err != nil {
		svc.log().Warn("VM stop error: %v", err)
	}
	if err := svc.MCP.Stop(ctx); err != nil {
		svc.log().Warn("MCPJungle stop error: %v", err)
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
	_ = os.Remove(filepath.Join(svc.Config.StateDir, "t3code-url"))
	if err := svc.VM.Destroy(ctx); err != nil {
		return err
	}
	if err := svc.MCP.Stop(ctx); err != nil {
		svc.log().Warn("MCPJungle stop error: %v", err)
	}
	svc.log().Success("VM destroyed")
	return nil
}

// Launch launches the configured AI agent in the VM for the current working directory.
// T3 Code, when enabled, is started by Start() as a background service and does not
// affect this path — the agent terminal always launches regardless.
func (svc *LifecycleService) Launch(ctx context.Context) error {
	cfg := svc.Config

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

	threshold := cfg.VM.BaseImageRebuildPromptAfterDuration
	if threshold != config.DisabledDuration && threshold > 0 {
		if err := svc.checkBaseImageAge(ctx); err != nil {
			return err
		}
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
	go func() {
		<-sigCh
		if sess != nil {
			sess.Remove()
		}
		os.Exit(0)
	}()

	vmDir := realCWD

	svc.log().Info("Host: %s", hostCWD)
	svc.log().Info("VM:   %s", vmDir)
	svc.log().Info("Launching %s in VM", svc.Provider.Description())

	providerDef := svc.AgentDefs[svc.Provider.Name()]
	providerCfg := make(map[string]any)
	if providerDef.LaunchCommand != "" {
		providerCfg["launch_command"] = providerDef.LaunchCommand
	}
	env := agent.LaunchEnv{
		VMProfile: svc.VM.Profile(),
		WorkDir:   vmDir,
		Config:    providerCfg,
	}

	resp, err := svc.Provider.Launch(ctx, env)
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

	if svc.T3Code.IsRunning() {
		svc.log().Success("T3 Code is already running at http://localhost:%d", cfg.T3Code.Port)
		return nil
	}

	// Daemonize t3 serve inside the VM. nohup + & ensures it survives the SSH
	// session closing. mise shims are on PATH via /etc/profile.d/aivm-path.sh
	// which is sourced by every login shell (all VM.Run calls use bash -lc).
	startScript := fmt.Sprintf(`
nohup t3 serve --host 127.0.0.1 --port %d > /tmp/t3code.log 2>&1 &
echo "t3 serve started with PID $!"
`, cfg.T3Code.Port)

	svc.log().Info("Starting T3 Code server in VM...")
	if err := svc.VM.Run(ctx, startScript, nil); err != nil {
		return fmt.Errorf("starting t3 serve in VM: %w", err)
	}

	svc.log().Info("Starting SSH port-forward tunnel...")
	if err := svc.T3Code.Launch(ctx, cfg.T3Code.Port); err != nil {
		return fmt.Errorf("starting T3 Code tunnel: %w", err)
	}

	// Wait for the server to print its pairing info (up to 30 s), then display
	// everything from "T3 Code server is ready." onwards — this skips the noisy
	// migration logs and bash login-shell warnings that precede it.
	pairingScript := `
for i in $(seq 1 60); do
    if grep -q "T3 Code server is ready" /tmp/t3code.log 2>/dev/null; then
        break
    fi
    sleep 0.5
done
sed -n '/T3 Code server is ready/,$p' /tmp/t3code.log 2>/dev/null || true
`
	pairingInfo, err := svc.VM.RunOutput(ctx, pairingScript, nil)
	if err != nil {
		svc.log().Warn("Could not read T3 Code pairing info: %v", err)
		svc.log().Success("T3 Code is running at http://localhost:%d", cfg.T3Code.Port)
	} else if strings.TrimSpace(pairingInfo) != "" {
		// Rewrite the VM-internal address to localhost (SSH-tunnel side) so every
		// URL the user sees is consistent and actually reachable from the host.
		displayInfo := strings.ReplaceAll(strings.TrimSpace(pairingInfo), "127.0.0.1", "localhost")
		// Persist the token-bearing URL so 'aivm status' can show it later.
		pairingURL := parsePairingURL(pairingInfo, cfg.T3Code.Port)
		_ = os.WriteFile(filepath.Join(cfg.StateDir, "t3code-url"), []byte(pairingURL), 0644)
		fmt.Fprintln(svc.log().Out, displayInfo)
	} else {
		svc.log().Success("T3 Code is running at http://localhost:%d", cfg.T3Code.Port)
	}
	return nil
}

// parsePairingURL extracts the "Pairing URL:" line from t3 serve startup output,
// rewrites the VM-internal address (127.0.0.1) to localhost (exposed by the SSH
// tunnel), and returns the result. Falls back to a bare URL if not found.
func parsePairingURL(output string, port int) string {
	for _, line := range strings.Split(output, "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "Pairing URL:"); ok {
			return strings.ReplaceAll(strings.TrimSpace(after), "127.0.0.1", "localhost")
		}
	}
	return fmt.Sprintf("http://localhost:%d", port)
}

// checkBaseImageAge prompts the user when the base image is older than the configured
// threshold. It may rebuild the current VM after confirming with the user.
func (svc *LifecycleService) checkBaseImageAge(ctx context.Context) error {
	cfg := svc.Config

	if !svc.Confirmer.IsInteractive() {
		return nil
	}
	if vmCreatedRecently(cfg.StateDir) {
		return nil
	}

	imgMgr := svc.imageManager()
	img := imgMgr.LoadBaseImage()
	if img == nil {
		return nil
	}

	threshold := cfg.VM.BaseImageRebuildPromptAfterDuration
	imgAge := time.Since(img.CreatedAt)
	if imgAge < threshold {
		return nil
	}

	ageDays := int(imgAge.Hours() / 24)
	thresholdDays := int(threshold.Hours() / 24)

	fmt.Fprintln(svc.log().Out)
	svc.log().Warn("Base image is %d day(s) old (threshold: %d days)", ageDays, thresholdDays)
	svc.log().Warn("Created: %s", img.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	if !promptYesNo(svc.log().Out, svc.Confirmer, "  → Rebuild base image for a clean environment? [y/N] ") {
		return nil
	}

	sessions, _ := svc.Sessions.List()
	if len(sessions) == 0 {
		return svc.rebuildCurrentVM(ctx)
	}

	fmt.Fprintf(svc.log().Out, "\n  You have %d active session(s).\n", len(sessions))
	if !promptYesNo(svc.log().Out, svc.Confirmer, "  Kill all sessions and rebuild now? [y/N] ") {
		svc.log().Info("Skipping base image rebuild.")
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
	return svc.rebuildCurrentVM(ctx)
}

// rebuildCurrentVM destroys the current VM, recreates it, and runs full bootstrap.
func (svc *LifecycleService) rebuildCurrentVM(ctx context.Context) error {
	svc.log().Step("Stopping current VM...")
	if err := svc.Stop(ctx); err != nil {
		svc.log().Warn("Stop error (continuing): %v", err)
	}

	svc.log().Step("Destroying VM for fresh rebuild...")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	svc.log().Step("Creating new VM and rebuilding base image...")
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

// RebuildImage rebuilds the base VM image by re-running bootstrap on a fresh VM.
func (svc *LifecycleService) RebuildImage(ctx context.Context, force bool) error {
	imgMgr := svc.imageManager()
	current := imgMgr.LoadBaseImage()

	fmt.Fprintln(svc.log().Out)
	svc.log().Warn("Base image rebuild requested.")
	if current != nil {
		svc.log().Warn("Current base image: id=%s, created %s",
			current.ID, current.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		svc.log().Warn("No existing base image found.")
	}

	sessions, _ := svc.Sessions.List()
	if len(sessions) > 0 {
		svc.log().Warn("%d active session(s) detected.", len(sessions))
	}

	if force {
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.log().Info("Sent SIGTERM to %d session(s).", len(killed))
		}
		return svc.doHardRebuild(ctx, imgMgr)
	}

	switch promptImageRebuild(svc.log().Out, svc.Confirmer, len(sessions)) {
	case imageRebuildHard:
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			svc.log().Info("Sent SIGTERM to %d session(s).", len(killed))
		}
		return svc.doHardRebuild(ctx, imgMgr)
	default:
		return nil
	}
}

// bootstrapFreshVM starts targetVM fresh, runs a full bootstrap, and saves the base image.
// It records the VM creation timestamp and image reference. Returns the saved image.
func (svc *LifecycleService) bootstrapFreshVM(ctx context.Context, targetVM vm.VM, imgMgr *vm.ImageManager) (*vm.BaseImage, error) {
	if err := startFreshVM(ctx, targetVM, svc.Config, svc.activeAgentDef()); err != nil {
		return nil, err
	}
	imgMgr.RecordCreation()
	if err := svc.fullBootstrap(ctx, targetVM, true); err != nil {
		return nil, err
	}
	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		return nil, fmt.Errorf("saving base image: %w", err)
	}
	imgMgr.RecordVMImageRef(img.ID)
	return img, nil
}

// doHardRebuild destroys the current VM, recreates it, and runs full bootstrap.
func (svc *LifecycleService) doHardRebuild(ctx context.Context, imgMgr *vm.ImageManager) error {
	svc.log().Step("Destroying existing VM")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	img, err := svc.bootstrapFreshVM(ctx, svc.VM, imgMgr)
	if err != nil {
		return err
	}

	svc.log().Success("Base image rebuilt: %s (id=%s)", img.SnapshotName, img.ID)
	svc.log().Info("Future VMs will start from this image.")
	return nil
}
