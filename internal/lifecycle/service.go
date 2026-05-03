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

	"aivm/internal/agent"
	"aivm/internal/config"
	"aivm/internal/integration"
	aivmlog "aivm/internal/log"
	"aivm/internal/mcp"
	"aivm/internal/monitor"
	"aivm/internal/plugin"
	"aivm/internal/session"
	"aivm/internal/vm"
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
	// Provider is the active AI agent provider selected from the config.
	Provider agent.Provider
	// Integrations is the complete list of integrations to evaluate during bootstrap.
	Integrations []integration.IntegrationDef
	// VMFactory creates VM instances for secondary profiles (e.g. rebuild VMs).
	VMFactory vm.VMFactory
	// Confirmer handles interactive terminal I/O. Use NewTTYConfirmer() in production,
	// NewScriptedConfirmer() in tests, or &SilentConfirmer{} for non-interactive daemons.
	Confirmer Confirmer
	// GetWorkDir returns the working directory for Launch. When nil, os.Getwd is used.
	GetWorkDir func() (string, error)
}

// Start starts the VM and all services, then runs bootstrap if needed.
func (svc *LifecycleService) Start(ctx context.Context) error {
	cfg := svc.Config

	aivmlog.Step("Starting aivm")

	aivmlog.Info("Ensuring MCPJungle is running...")
	if err := svc.MCP.Start(ctx); err != nil {
		return fmt.Errorf("starting MCPJungle: %w", err)
	}

	opts := buildStartOptions(cfg)

	status, err := svc.VM.Status(ctx)
	if err != nil {
		return err
	}

	if status == vm.StatusStopped && svc.shouldRecreateVM() {
		aivmlog.Step("Deleting aged VM profile '%s'", cfg.VM.Profile)
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

	imgMgr := vm.NewImageManager(svc.VM, cfg.StateDir)
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
		aivmlog.Warn("could not start idle monitor: %v", err)
	}

	aivmlog.Success("aivm is ready")
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
		aivmlog.Warn("could not save base image (non-fatal): %v", err)
	} else {
		imgMgr.RecordVMImageRef(img.ID)
	}
	return nil
}

// shouldRecreateVM prompts the user when the VM has exceeded its configured age threshold.
func (svc *LifecycleService) shouldRecreateVM() bool {
	cfg := svc.Config
	if cfg.VM.MaxAgeDays <= 0 {
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
	age := int(time.Since(time.Unix(epoch, 0)).Hours() / 24)
	if age < cfg.VM.MaxAgeDays {
		return false
	}
	return promptVMAge(svc.Confirmer, cfg.VM.Profile, age, cfg.VM.MaxAgeDays) == vmAgeRecreate
}

// Stop stops the VM and all services.
func (svc *LifecycleService) Stop(ctx context.Context) error {
	aivmlog.Step("Stopping aivm")
	svc.Monitor.Stop()
	if err := svc.VM.Stop(ctx); err != nil {
		aivmlog.Warn("VM stop error: %v", err)
	}
	if err := svc.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("aivm stopped")
	return nil
}

// Destroy deletes the VM and stops all services.
func (svc *LifecycleService) Destroy(ctx context.Context) error {
	svc.Monitor.Stop()
	if err := svc.VM.Destroy(ctx); err != nil {
		return err
	}
	if err := svc.MCP.Stop(ctx); err != nil {
		aivmlog.Warn("MCPJungle stop error: %v", err)
	}
	aivmlog.Success("VM destroyed")
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
	devRoot := cfg.VM.DevRoot
	realCWD, _ := filepath.EvalSymlinks(hostCWD)
	realDev, _ := filepath.EvalSymlinks(devRoot)

	if !strings.HasPrefix(realCWD, realDev) {
		return fmt.Errorf("current directory '%s' is not under AIVM_DEV_ROOT (%s)\naivm only works inside %s", realCWD, devRoot, devRoot)
	}

	// If a transition is already in progress, route this session to the new VM.
	if ts := vm.LoadTransitionState(cfg.StateDir); ts != nil {
		aivmlog.Info("Transition active: launching on new VM '%s' (legacy '%s' still draining)", ts.NewProfile, ts.LegacyProfile)
		svc.VM = svc.VMFactory(ts.NewProfile, cfg.StateDir)
	} else if cfg.VM.BaseImageMaxAgeDays > 0 {
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
		aivmlog.Warn("could not create session lock: %v", err)
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

	aivmlog.Info("Host: %s", hostCWD)
	aivmlog.Info("VM:   %s", vmDir)
	aivmlog.Step("Launching %s in VM", svc.Provider.Description())

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
// threshold. It may rebuild the current VM (option 1) or start a parallel transition (option 2).
func (svc *LifecycleService) checkBaseImageAge(ctx context.Context) error {
	cfg := svc.Config

	if !svc.Confirmer.IsInteractive() {
		return nil
	}
	if vmCreatedRecently(cfg.StateDir) {
		return nil
	}

	imgMgr := vm.NewImageManager(svc.VM, cfg.StateDir)
	ageDays := imgMgr.BaseImageAgeDays()
	if ageDays < cfg.VM.BaseImageMaxAgeDays {
		return nil
	}

	img := imgMgr.LoadBaseImage()
	if img == nil {
		return nil
	}

	fmt.Println()
	aivmlog.Warn("Base image is %d day(s) old (threshold: %d days)", ageDays, cfg.VM.BaseImageMaxAgeDays)
	aivmlog.Warn("Created: %s", img.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	if !promptYesNo(svc.Confirmer, "  → Rebuild base image for a clean environment? [y/N] ") {
		return nil
	}

	sessions, _ := svc.Sessions.List()
	if len(sessions) == 0 {
		return svc.rebuildCurrentVM(ctx)
	}

	switch promptBaseImageRebuildWithSessions(svc.Confirmer, len(sessions)) {
	case baseImageRebuildNow:
		aivmlog.Step("Killing %d active session(s)...", len(sessions))
		for _, s := range sessions {
			proc, err := os.FindProcess(s.PID)
			if err == nil {
				proc.Signal(syscall.SIGTERM)
			}
			s.Remove()
		}
		return svc.rebuildCurrentVM(ctx)
	case baseImageTransition:
		return svc.startTransitionVM(ctx)
	default:
		aivmlog.Info("Skipping base image rebuild.")
		return nil
	}
}

// rebuildCurrentVM destroys the current VM, recreates it, and runs full bootstrap.
func (svc *LifecycleService) rebuildCurrentVM(ctx context.Context) error {
	aivmlog.Step("Stopping current VM...")
	if err := svc.Stop(ctx); err != nil {
		aivmlog.Warn("Stop error (continuing): %v", err)
	}

	aivmlog.Step("Destroying VM for fresh rebuild...")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	aivmlog.Step("Creating new VM and rebuilding base image...")
	return svc.Start(ctx)
}

// startTransitionVM creates a second VM with a fresh bootstrap while the current VM
// keeps running for existing sessions. Future launch sessions use the new VM.
func (svc *LifecycleService) startTransitionVM(ctx context.Context) error {
	cfg := svc.Config
	newProfile := cfg.VM.Profile + "-next"
	transitionStart := time.Now()

	aivmlog.Step("Creating new VM '%s' with fresh base image...", newProfile)

	newVM := svc.VMFactory(newProfile, cfg.StateDir)
	_ = newVM.Destroy(ctx)

	imgMgr := vm.NewImageManager(newVM, cfg.StateDir)
	img, err := svc.bootstrapFreshVM(ctx, newVM, imgMgr)
	if err != nil {
		return err
	}
	if img != nil {
		aivmlog.Success("New base image saved: id=%s", img.ID)
	}

	ts := &vm.TransitionState{
		LegacyProfile: cfg.VM.Profile,
		NewProfile:    newProfile,
		StartedAt:     transitionStart,
	}
	if err := vm.SaveTransitionState(cfg.StateDir, ts); err != nil {
		return fmt.Errorf("saving transition state: %w", err)
	}

	if err := svc.Monitor.EnsureLegacyMonitorRunning(); err != nil {
		aivmlog.Warn("could not start legacy monitor: %v", err)
	}

	aivmlog.Success("Transition started: new sessions use '%s', old VM '%s' drains automatically", newProfile, cfg.VM.Profile)

	svc.VM = newVM
	return nil
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
		state, _ := loadBootstrapState(svc.Config.StateDir)
		if state != nil {
			state.Installed = mergeStrings(state.Installed, []string{onlyPlugin})
			_ = saveBootstrapState(svc.Config.StateDir, state)
		}
		return nil
	}

	if force {
		return svc.fullBootstrap(ctx, svc.VM, true)
	}

	return svc.syncBootstrap(ctx)
}

// RebuildImage rebuilds the base VM image by re-running bootstrap on a fresh VM.
func (svc *LifecycleService) RebuildImage(ctx context.Context, force bool) error {
	cfg := svc.Config
	imgMgr := vm.NewImageManager(svc.VM, cfg.StateDir)
	current := imgMgr.LoadBaseImage()

	fmt.Println()
	aivmlog.Warn("Base image rebuild requested.")
	if current != nil {
		aivmlog.Warn("Current base image: id=%s, created %s",
			current.ID, current.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		aivmlog.Warn("No existing base image found.")
	}

	sessions, _ := svc.Sessions.List()
	if len(sessions) > 0 {
		aivmlog.Warn("%d active session(s) detected.", len(sessions))
	}

	if force {
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			aivmlog.Info("Sent SIGTERM to %d session(s).", len(killed))
		}
		return svc.doHardRebuild(ctx, imgMgr)
	}

	switch promptImageRebuild(svc.Confirmer, len(sessions)) {
	case imageRebuildHard:
		if len(sessions) > 0 {
			killed := svc.Sessions.KillAll()
			aivmlog.Info("Sent SIGTERM to %d session(s).", len(killed))
		}
		return svc.doHardRebuild(ctx, imgMgr)
	case imageRebuildSoft:
		return svc.doSoftRebuild(ctx, imgMgr)
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
	cfg := svc.Config

	aivmlog.Step("Destroying existing VM")
	if err := svc.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	img, err := svc.bootstrapFreshVM(ctx, svc.VM, imgMgr)
	if err != nil {
		return err
	}

	vm.ClearTransitionState(cfg.StateDir)
	aivmlog.Success("Base image rebuilt: %s (id=%s)", img.SnapshotName, img.ID)
	aivmlog.Info("Future VMs will start from this image.")
	return nil
}

// doSoftRebuild bootstraps a temporary second VM so active sessions are not disrupted.
func (svc *LifecycleService) doSoftRebuild(ctx context.Context, imgMgr *vm.ImageManager) error {
	cfg := svc.Config
	tempProfile := cfg.VM.Profile + "-rebuild"
	tempVM := svc.VMFactory(tempProfile, cfg.StateDir)

	_ = tempVM.Destroy(ctx)

	aivmlog.Step("Starting temporary rebuild VM '%s'", tempProfile)
	if err := startFreshVM(ctx, tempVM, cfg); err != nil {
		return err
	}

	aivmlog.Step("Bootstrapping rebuild VM from scratch")
	if err := svc.rebuildBootstrap(ctx, tempVM); err != nil {
		_ = tempVM.Destroy(ctx)
		return err
	}

	if _, err := imgMgr.SaveBaseImageMetadataOnly(); err != nil {
		_ = tempVM.Destroy(ctx)
		return fmt.Errorf("saving base image metadata: %w", err)
	}

	aivmlog.Step("Destroying temporary rebuild VM")
	_ = tempVM.Destroy(ctx)

	ts := &vm.TransitionState{
		LegacyProfile: cfg.VM.Profile,
		NewProfile:    cfg.VM.Profile,
		StartedAt:     time.Now(),
	}
	if err := vm.SaveTransitionState(cfg.StateDir, ts); err != nil {
		aivmlog.Warn("could not save transition state: %v", err)
	}
	if err := svc.Monitor.EnsureLegacyMonitorRunning(); err != nil {
		aivmlog.Warn("could not start legacy monitor: %v", err)
	}

	aivmlog.Success("New base image recorded.")
	aivmlog.Info("Legacy VM '%s' will be removed once all sessions close.", cfg.VM.Profile)
	aivmlog.Info("Run 'aivm start' after that to apply the new image.")
	return nil
}
