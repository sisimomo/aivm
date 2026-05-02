package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"aivm/internal/agent"
	"aivm/internal/bootstrap"
	aivmlog "aivm/internal/log"
	"aivm/internal/plugin"
	"aivm/internal/vm"
)

// BootstrapState is persisted on the host (no SSH required) and records which
// plugins have been installed in the current VM. Subsequent aivm invocations
// compare the desired plugin list against this state and skip bootstrap entirely
// when nothing has changed.
type BootstrapState struct {
	Version   string   `json:"version"`
	Provider  string   `json:"provider"`
	Installed []string `json:"installed"`
}

func bootstrapStatePath(stateDir string) string {
	return filepath.Join(stateDir, "bootstrap-state.json")
}

func loadBootstrapState(stateDir string) (*BootstrapState, error) {
	data, err := os.ReadFile(bootstrapStatePath(stateDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s BootstrapState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, nil // treat corrupt state as absent
	}
	return &s, nil
}

func saveBootstrapState(stateDir string, s *BootstrapState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(bootstrapStatePath(stateDir), data, 0644)
}

func clearBootstrapState(stateDir string) {
	_ = os.Remove(bootstrapStatePath(stateDir))
}

// recordBootstrapState saves a BootstrapState reflecting the current desired
// plugin set for app's provider. Call this after any successful full bootstrap.
func recordBootstrapState(app *App) error {
	desired := bootstrapEnabledPlugins(app.Registry, app.Provider, app.Config.Plugins.Enabled)
	return saveBootstrapState(app.Config.StateDir, &BootstrapState{
		Version:   bootstrap.BootstrapVersion,
		Provider:  app.Provider.Name(),
		Installed: desired,
	})
}

// newBootstrapEngine builds a bootstrap.Engine targeting targetVM.
// plugins is an explicit list of plugins to enable; nil means "all enabled
// plugins for the configured provider" (resolved by bootstrapEnabledPlugins).
func newBootstrapEngine(app *App, targetVM vm.VM, plugins []string) *bootstrap.Engine {
	enabled := plugins
	if enabled == nil {
		enabled = bootstrapEnabledPlugins(app.Registry, app.Provider, app.Config.Plugins.Enabled)
	}
	return &bootstrap.Engine{
		VM: targetVM,
		Executor: &plugin.Executor{
			Registry:       app.Registry,
			Enabled:        enabled,
			PluginConfig:   app.Config.Plugins.Config,
			StateDir:       app.Config.StateDir,
			ActiveProvider: app.Provider.Name(),
			VMInst:         targetVM,
		},
		StateDir: app.Config.StateDir,
	}
}

func bootstrapEnabledPlugins(reg *plugin.Registry, provider agent.Provider, configured []string) []string {
	active := provider.Name()
	enabled := make([]string, 0, len(configured)+len(provider.RequiredPlugins()))
	seen := make(map[string]bool, len(configured)+len(provider.RequiredPlugins()))

	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		enabled = append(enabled, name)
	}

	for _, name := range configured {
		if p, ok := reg.Get(name); ok {
			if agents := p.Agents(); len(agents) > 0 && !containsString(agents, active) {
				continue
			}
		}
		add(name)
	}

	for _, name := range provider.RequiredPlugins() {
		add(name)
	}

	return enabled
}

// fullBootstrap runs all configured plugins on targetVM and saves bootstrap
// state on success. Use force=true for a fresh blank VM (skips per-plugin
// Check calls); force=false for an existing VM with unknown state (uses
// Check to skip already-installed plugins).
func fullBootstrap(ctx context.Context, app *App, targetVM vm.VM, force bool) error {
	eng := newBootstrapEngine(app, targetVM, nil)
	if err := eng.Run(ctx, force); err != nil {
		return err
	}
	return recordBootstrapState(app)
}

// syncBootstrap is the main entry point called on every aivm invocation.
// It reads the host-side state file (no SSH) and returns immediately when
// nothing has changed, runs only newly-added plugins when the plugin list
// grew, or handles an agent mismatch interactively. It returns true when
// the VM was recreated so callers can jump past normal boot steps.
func syncBootstrap(ctx context.Context, app *App) (bool, error) {
	state, err := loadBootstrapState(app.Config.StateDir)
	if err != nil {
		aivmlog.Warn("could not read bootstrap state, running full bootstrap: %v", err)
	}

	if state == nil || state.Version != bootstrap.BootstrapVersion {
		// No state or stale schema: Check()-based reconcile so we do not
		// reinstall plugins that the VM already has.
		return false, fullBootstrap(ctx, app, app.VM, false)
	}

	desired := bootstrapEnabledPlugins(app.Registry, app.Provider, app.Config.Plugins.Enabled)

	// Mismatch: the configured agent's plugin is absent but another agent's is present.
	if !requiredPluginsInstalled(app.Provider, state.Installed) {
		installed := installedProvidersFromState(app, state)
		delete(installed, app.Provider.Name())
		if len(installed) > 0 {
			return resolveAgentMismatch(ctx, app, state, installed)
		}
		// No other agent found; the state predates agent tracking — fall
		// through to the new-plugin install path below.
	}

	// Compute plugins that are new since the last recorded state.
	installedSet := stringSet(state.Installed)
	var newPlugins []string
	for _, p := range desired {
		if !installedSet[p] {
			newPlugins = append(newPlugins, p)
		}
	}

	if len(newPlugins) == 0 {
		aivmlog.Info("VM is up to date — skipping bootstrap")
		return false, nil
	}

	aivmlog.Step("Installing %d new plugin(s): %s", len(newPlugins), strings.Join(newPlugins, ", "))
	eng := newBootstrapEngine(app, app.VM, newPlugins)
	if err := eng.Run(ctx, false); err != nil {
		return false, err
	}
	state.Installed = mergeStrings(state.Installed, newPlugins)
	state.Provider = app.Provider.Name()
	return false, saveBootstrapState(app.Config.StateDir, state)
}

func resolveAgentMismatch(ctx context.Context, app *App, state *BootstrapState, otherInstalled map[string]bool) (bool, error) {
	installedDescriptions := installedProviderDescriptions(app, otherInstalled)
	configured := app.Provider.Description()
	installedSummary := strings.Join(installedDescriptions, ", ")

	aivmlog.Warn("VM '%s' was created for a different agent", app.VM.Profile())
	if len(installedDescriptions) == 1 {
		aivmlog.Warn("Installed agent: %s", installedSummary)
	} else {
		aivmlog.Warn("Installed agents: %s", installedSummary)
	}
	aivmlog.Warn("Configured agent: %s", configured)

	if !interactive(app) {
		return false, fmt.Errorf(
			"VM %q was created for %s, but config selects %s; rerun interactively to choose whether to install %s into the existing VM or recreate it with only %s",
			app.VM.Profile(),
			installedSummary,
			configured,
			configured,
			configured,
		)
	}

	sessions, _ := app.Sessions.List()

	fmt.Println()
	fmt.Printf("  This VM already has %s installed.\n", installedSummary)
	fmt.Printf("  Config now selects %s.\n", configured)
	if len(sessions) > 0 {
		fmt.Printf("  Note: option 2 will terminate %d active session(s).\n", len(sessions))
	}
	fmt.Printf("  Choose how to proceed:\n")
	fmt.Printf("    1. Install %s in the existing VM and keep the current agent(s)\n", configured)
	fmt.Printf("    2. Delete the VM and recreate it with only %s\n", configured)
	fmt.Printf("  Choice [1/2]: ")
	choice := readAnswer(app)

	switch choice {
	case "1":
		// Install only the newly-required plugins (agent plugin + any other new ones).
		desired := bootstrapEnabledPlugins(app.Registry, app.Provider, app.Config.Plugins.Enabled)
		installedSet := stringSet(state.Installed)
		var newPlugins []string
		for _, p := range desired {
			if !installedSet[p] {
				newPlugins = append(newPlugins, p)
			}
		}
		eng := newBootstrapEngine(app, app.VM, newPlugins)
		if err := eng.Run(ctx, false); err != nil {
			return false, err
		}
		state.Installed = mergeStrings(state.Installed, newPlugins)
		state.Provider = app.Provider.Name()
		return false, saveBootstrapState(app.Config.StateDir, state)
	case "2":
		return true, recreateVMForConfiguredAgent(ctx, app)
	default:
		return false, fmt.Errorf("invalid choice %q", choice)
	}
}

func recreateVMForConfiguredAgent(ctx context.Context, app *App) error {
	sessions, _ := app.Sessions.List()
	if len(sessions) > 0 {
		aivmlog.Step("Terminating %d active session(s)", len(sessions))
		for _, sess := range sessions {
			proc, err := os.FindProcess(sess.PID)
			if err == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
			sess.Remove()
		}
	}

	clearBootstrapState(app.Config.StateDir)

	aivmlog.Step("Recreating VM for %s", app.Provider.Description())
	if err := app.VM.Destroy(ctx); err != nil {
		return fmt.Errorf("destroying VM: %w", err)
	}
	if err := rebuildStartVM(ctx, app.VM, app.Config); err != nil {
		return err
	}

	if err := fullBootstrap(ctx, app, app.VM, true); err != nil {
		return err
	}

	writeVMCreatedAt(app.Config.StateDir, time.Now())
	app.Sessions.ClearVMStoppedAt()
	vm.ClearTransitionState(app.Config.StateDir)

	imgMgr := vm.NewImageManager(app.VM, app.Config.StateDir)
	img, err := imgMgr.SaveBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("saving base image: %w", err)
	}
	imgMgr.RecordVMImageRef(img.ID)

	aivmlog.Success("VM recreated with only %s", app.Provider.Description())
	return nil
}

// requiredPluginsInstalled returns true when all of provider's required plugins
// are present in the installed list.
func requiredPluginsInstalled(provider agent.Provider, installed []string) bool {
	set := stringSet(installed)
	for _, p := range provider.RequiredPlugins() {
		if !set[p] {
			return false
		}
	}
	return true
}

// installedProvidersFromState derives which agent providers are fully installed
// based on the host-side bootstrap state (no SSH required).
func installedProvidersFromState(app *App, state *BootstrapState) map[string]bool {
	installedSet := stringSet(state.Installed)
	result := make(map[string]bool)
	for name, provider := range app.Agents.All() {
		required := provider.RequiredPlugins()
		if len(required) == 0 {
			continue
		}
		allPresent := true
		for _, p := range required {
			if !installedSet[p] {
				allPresent = false
				break
			}
		}
		if allPresent {
			result[name] = true
		}
	}
	return result
}

func writeVMCreatedAt(stateDir string, at time.Time) {
	agePath := filepath.Join(stateDir, "vm-created-at")
	_ = os.WriteFile(agePath, []byte(fmt.Sprintf("%d", at.Unix())), 0644)
}

func installedProviderDescriptions(app *App, installed map[string]bool) []string {
	providers := app.Agents.All()
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		if provider, ok := providers[name]; ok {
			out = append(out, provider.Description())
		} else {
			out = append(out, name)
		}
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

func mergeStrings(base, additions []string) []string {
	set := stringSet(base)
	result := make([]string, len(base))
	copy(result, base)
	for _, s := range additions {
		if !set[s] {
			result = append(result, s)
			set[s] = true
		}
	}
	return result
}
