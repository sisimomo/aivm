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
	"github.com/sisimomo/aivm/internal/vm"
)

// LifecycleService owns all orchestration logic for the aivm VM lifecycle.
// CLI commands are thin adapters that call into this service.
type LifecycleService struct {
	Config   *config.Config
	VM       vm.VM
	MCP      mcp.MCPManager
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

// Start starts the VM and all services, then runs bootstrap if needed.
func (svc *LifecycleService) Start(ctx context.Context) error {
	cfg := svc.Config

	svc.log().Step("Starting aivm")

	svc.log().Info("Ensuring MCPJungle is running...")
	if err := svc.MCP.Start(ctx); err != nil {
		return fmt.Errorf("starting MCPJungle: %w", err)
	}

	opts := buildStartOptions(cfg)

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

	os.MkdirAll(filepath.Join(cfg.StateDir, ".claude", "projects"), 0755)

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

	if err := svc.Monitor.EnsureRunning(); err != nil {
		svc.log().Warn("could not start idle monitor: %v", err)
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
			proc.Signal(syscall.SIGTERM)
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
	if err := startFreshVM(ctx, targetVM, svc.Config); err != nil {
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

