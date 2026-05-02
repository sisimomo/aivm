package cli

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

	"github.com/spf13/cobra"

	"aivm/internal/agent"
	"aivm/internal/bootstrap"
	aivmlog "aivm/internal/log"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

func LaunchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "launch [directory]",
		Short: "Launch Claude Code in the VM (default command)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoLaunch(cmd.Context(), app)
		},
	}
}

func DoLaunch(ctx context.Context, app *App) error {
	cfg := app.Config

	hostCWD, _ := os.Getwd()
	devRoot := cfg.VM.DevRoot
	realCWD, _ := filepath.EvalSymlinks(hostCWD)
	realDev, _ := filepath.EvalSymlinks(devRoot)

	if !strings.HasPrefix(realCWD, realDev) {
		return fmt.Errorf("current directory '%s' is not under AIVM_DEV_ROOT (%s)\naivm only works inside %s", realCWD, devRoot, devRoot)
	}

	// If a transition is already in progress, route this session to the new VM.
	if ts := vm.LoadTransitionState(cfg.StateDir); ts != nil {
		aivmlog.Info("Transition active: launching on new VM '%s' (legacy '%s' still draining)", ts.NewProfile, ts.LegacyProfile)
		app.VM = vm.NewColima(ts.NewProfile, cfg.StateDir)
	} else if cfg.VM.BaseImageMaxAgeDays > 0 {
		// Check if the base image needs rebuilding before starting a new session.
		if err := checkBaseImageAge(ctx, app); err != nil {
			return err
		}
	}

	status, err := app.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	sess, err := app.Sessions.Create(hostCWD)
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
	aivmlog.Step("Launching %s in VM", app.Provider.Description())

	providerCfg := cfg.Agent.Providers[app.Provider.Name()]
	env := agent.LaunchEnv{
		VMProfile: app.VM.Profile(),
		WorkDir:   vmDir,
		Config:    providerCfg,
	}

	resp, err := app.Provider.Launch(ctx, env)
	if err != nil {
		return err
	}
	if resp != nil && resp.ExitCode != 0 {
		return fmt.Errorf("agent exited with code %d", resp.ExitCode)
	}
	return nil
}

// checkBaseImageAge prompts the user when the base image is older than the configured
// threshold. It may rebuild the current VM in place (option 1) or start a parallel
// transition to a new VM (option 2), updating app.VM accordingly.
func checkBaseImageAge(ctx context.Context, app *App) error {
	cfg := app.Config

	if !isTerminal() {
		return nil
	}

	// Skip if the VM was created very recently (e.g. DoStart just ran a rebuild).
	if vmCreatedRecently(cfg.StateDir) {
		return nil
	}

	imgMgr := vm.NewImageManager(app.VM, cfg.StateDir)
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
	fmt.Printf("  → Rebuild base image for a clean environment? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		return nil
	}

	sessions, _ := app.Sessions.List()
	if len(sessions) == 0 {
		return rebuildCurrentVM(ctx, app)
	}

	fmt.Printf("\n  You have %d active session(s).\n", len(sessions))
	fmt.Printf("  Choose how to proceed:\n")
	fmt.Printf("    1. Kill all sessions and rebuild now (sessions will be lost)\n")
	fmt.Printf("    2. Start a new VM with the fresh image; old VM runs until sessions end, then auto-deletes\n")
	fmt.Printf("  Choice [1/2]: ")
	var choice string
	fmt.Scanln(&choice)

	switch choice {
	case "1":
		aivmlog.Step("Killing %d active session(s)...", len(sessions))
		for _, s := range sessions {
			proc, err := os.FindProcess(s.PID)
			if err == nil {
				proc.Signal(syscall.SIGTERM)
			}
			s.Remove()
		}
		return rebuildCurrentVM(ctx, app)
	case "2":
		return startTransitionVM(ctx, app)
	default:
		aivmlog.Info("Skipping base image rebuild.")
		return nil
	}
}

// rebuildCurrentVM destroys the current VM, recreates it, runs full bootstrap,
// and saves a new base image. app.VM continues to point to the same profile.
func rebuildCurrentVM(ctx context.Context, app *App) error {
	aivmlog.Step("Stopping current VM...")
	if err := DoStop(ctx, app); err != nil {
		aivmlog.Warn("Stop error (continuing): %v", err)
	}

	aivmlog.Step("Destroying VM for fresh rebuild...")
	if err := app.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}

	aivmlog.Step("Creating new VM and rebuilding base image...")
	return DoStart(ctx, app)
}

// startTransitionVM creates a second VM with a fresh bootstrap while the current VM
// keeps running for existing sessions. Future launch sessions use the new VM.
// The legacy VM is destroyed automatically once all pre-transition sessions end.
func startTransitionVM(ctx context.Context, app *App) error {
	cfg := app.Config
	newProfile := cfg.VM.Profile + "-next"
	transitionStart := time.Now()

	aivmlog.Step("Creating new VM '%s' with fresh base image...", newProfile)

	newVM := vm.NewColima(newProfile, cfg.StateDir)

	opts := vm.StartOptions{
		CPUs:      cfg.VM.CPUs,
		MemoryGiB: cfg.VM.MemoryGiB,
		DiskGiB:   cfg.VM.DiskGiB,
		VMType:    cfg.VM.Type,
		Mounts: []vm.Mount{
			{HostPath: cfg.VM.DevRoot, Writable: true},
			{HostPath: filepath.Join(cfg.StateDir, ".claude", "projects"), Writable: true},
		},
	}

	if err := newVM.Start(ctx, opts); err != nil {
		return fmt.Errorf("starting new VM: %w", err)
	}

	if err := newVM.WaitReady(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("waiting for new VM: %w", err)
	}

	// Record vm-created-at for the new VM.
	os.WriteFile(filepath.Join(cfg.StateDir, "vm-created-at"),
		[]byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)

	// Full bootstrap on the new VM.
	eng := &bootstrap.Engine{
		VM: newVM,
		Executor: &plugin.Executor{
			Registry:       app.Registry,
			Enabled:        cfg.Plugins.Enabled,
			PluginConfig:   cfg.Plugins.Config,
			StateDir:       cfg.StateDir,
			ActiveProvider: app.Provider.Name(),
			VMInst:         newVM,
		},
		StateDir: cfg.StateDir,
	}
	if err := eng.Run(ctx, true); err != nil {
		return fmt.Errorf("bootstrap new VM: %w", err)
	}

	imgMgr := vm.NewImageManager(newVM, cfg.StateDir)
	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		aivmlog.Warn("could not save base image (non-fatal): %v", err)
	} else {
		imgMgr.RecordVMImageRef(img.ID)
		aivmlog.Success("New base image saved: id=%s", img.ID)
	}

	// Persist the transition so future invocations know to use the new VM.
	ts := &vm.TransitionState{
		LegacyProfile: cfg.VM.Profile,
		NewProfile:    newProfile,
		StartedAt:     transitionStart,
	}
	if err := vm.SaveTransitionState(cfg.StateDir, ts); err != nil {
		return fmt.Errorf("saving transition state: %w", err)
	}

	// Start the background legacy monitor.
	if err := app.Monitor.EnsureLegacyMonitorRunning(); err != nil {
		aivmlog.Warn("could not start legacy monitor: %v", err)
	}

	aivmlog.Success("Transition started: new sessions use '%s', old VM '%s' drains automatically", newProfile, cfg.VM.Profile)

	// Point app.VM at the new profile for this session.
	app.VM = newVM
	return nil
}

// vmCreatedRecently returns true when the current VM was created within the last 10 minutes,
// indicating that DoStart just ran a fresh bootstrap and a base image rebuild is unnecessary.
func vmCreatedRecently(stateDir string) bool {
	data, err := os.ReadFile(filepath.Join(stateDir, "vm-created-at"))
	if err != nil {
		return false
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(epoch, 0)) < 10*time.Minute
}
