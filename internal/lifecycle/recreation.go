package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sisimomo/aivm/internal/config"
	aivmlog "github.com/sisimomo/aivm/internal/log"
	"github.com/sisimomo/aivm/internal/vm"
)

const BootstrapAgeUnknown = time.Duration(-2)

type TimerDue struct {
	BootstrapDue bool
	VMAgeDue     bool
}

type RecreationAction int

const (
	ActionResume RecreationAction = iota
	ActionFastRecreate
	ActionFullBootstrap
	ActionPromptBootstrapRefresh
	ActionPromptVMAge
	ActionPromptCombined
	ActionPromptRuntimeChange
	ActionPromptConfigChange
)

func EvaluateTimers(vmAge, vmThreshold, bootstrapAge, bootstrapThreshold time.Duration) TimerDue {
	due := TimerDue{}
	if vmThreshold > 0 && vmAge >= vmThreshold && vmAge >= 0 {
		due.VMAgeDue = true
	}
	if bootstrapThreshold > 0 && bootstrapAge >= bootstrapThreshold && bootstrapAge >= 0 {
		due.BootstrapDue = true
	}
	return due
}

func (svc *LifecycleService) readBootstrapAge() time.Duration {
	return readAgeFile(filepath.Join(svc.Config.StateDir, vm.BootstrapAtFile), BootstrapAgeUnknown)
}

func (svc *LifecycleService) readVMAge() time.Duration {
	return readAgeFile(filepath.Join(svc.Config.StateDir, vm.VMCreatedAtFile), time.Duration(-1))
}

func readAgeFile(path string, unknown time.Duration) time.Duration {
	data, err := os.ReadFile(path)
	if err != nil {
		return unknown
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return unknown
	}
	return time.Since(time.Unix(epoch, 0))
}

func (svc *LifecycleService) runtimeChanged(state *BootstrapState) bool {
	if state == nil {
		return false
	}
	return state.Backend != effectiveBackend(svc.Config.VM) || state.VMType != effectiveVMType(svc.Config.VM)
}

func (svc *LifecycleService) decideStartAction(ctx context.Context, status vm.Status) (RecreationAction, error) {
	state, err := loadBootstrapState(svc.Config.StateDir)
	if err != nil {
		svc.logger().Warn("could not load bootstrap state for recreation decision")
	}
	interactive := svc.Confirmer != nil && svc.Confirmer.IsInteractive()

	if svc.runtimeChanged(state) {
		if interactive {
			return ActionPromptRuntimeChange, nil
		}
		return ActionResume, nil
	}

	configChanged := state != nil && (state.Provider != svc.Provider.Name() || state.ConfigHash != svc.currentConfigHash())
	if configChanged {
		svc.deleteBaseImage(ctx)
		if interactive {
			return ActionPromptConfigChange, nil
		}
		return ActionResume, nil
	}

	timers := EvaluateTimers(
		svc.readVMAge(),
		svc.Config.VM.RecreatePromptAfterDuration,
		svc.readBootstrapAge(),
		svc.Config.VM.BootstrapRefreshPromptAfterDuration,
	)

	if interactive {
		if timers.BootstrapDue && timers.VMAgeDue {
			return ActionPromptCombined, nil
		}
		if timers.BootstrapDue {
			return ActionPromptBootstrapRefresh, nil
		}
		if timers.VMAgeDue && status != vm.StatusNotFound {
			return ActionPromptVMAge, nil
		}
	}

	if status == vm.StatusNotFound {
		if svc.hasValidBase(ctx) {
			return ActionFastRecreate, nil
		}
		return ActionFullBootstrap, nil
	}

	if status == vm.StatusStopped && interactive && timers.VMAgeDue {
		return ActionPromptVMAge, nil
	}

	return ActionResume, nil
}

func (svc *LifecycleService) handleRecreationPrompt(ctx context.Context, action RecreationAction, status vm.Status) error {
	switch action {
	case ActionPromptRuntimeChange:
		state, _ := loadBootstrapState(svc.Config.StateDir)
		from := runtimeLabel("", "")
		if state != nil {
			from = runtimeLabel(state.Backend, state.VMType)
		}
		to := runtimeLabel(effectiveBackend(svc.Config.VM), effectiveVMType(svc.Config.VM))
		if !PromptRuntimeChange(aivmlog.TerminalOut(), svc.Confirmer, from, to) {
			return nil
		}
		svc.deleteBaseImage(ctx)
		clearBootstrapState(svc.Config.StateDir)
		vm.ClearHostAgeState(svc.Config.StateDir)
		return svc.recreateVM(ctx)
	case ActionPromptConfigChange:
		if err := svc.resolveConfigChange(ctx); err != nil {
			return err
		}
		currentStatus, err := svc.VM.Status(ctx)
		if err != nil {
			return err
		}
		if currentStatus == vm.StatusRunning {
			return nil
		}
		svc.skipConfigChangePrompt = true
		defer func() { svc.skipConfigChangePrompt = false }()
		return svc.resumeOrStartVM(ctx, currentStatus)
	case ActionPromptVMAge:
		threshold := svc.Config.VM.RecreatePromptAfterDuration
		if threshold == config.DisabledDuration || threshold <= 0 {
			return svc.resumeOrStartVM(ctx, status)
		}
		vmAge := svc.readVMAge()
		if vmAge < 0 {
			return svc.resumeOrStartVM(ctx, status)
		}
		if promptVMAge(svc.Confirmer, svc.VM.Profile(), vmAge, threshold) == vmAgeRecreate {
			if svc.hasValidBase(ctx) {
				return svc.fastRecreate(ctx)
			}
			return svc.fullBootstrap(ctx)
		}
		return svc.resumeOrStartVM(ctx, status)
	case ActionPromptBootstrapRefresh:
		threshold := svc.Config.VM.BootstrapRefreshPromptAfterDuration
		bootstrapAge := svc.readBootstrapAge()
		if PromptBootstrapRefresh(aivmlog.TerminalOut(), svc.Confirmer, bootstrapAge, threshold) {
			return svc.fullBootstrap(ctx)
		}
		if status == vm.StatusRunning {
			return nil
		}
		return svc.fastRecreate(ctx)
	case ActionPromptCombined:
		vmThreshold := svc.Config.VM.RecreatePromptAfterDuration
		bootstrapThreshold := svc.Config.VM.BootstrapRefreshPromptAfterDuration
		vmAge := svc.readVMAge()
		bootstrapAge := svc.readBootstrapAge()
		choice := PromptCombined(
			aivmlog.TerminalOut(),
			svc.Confirmer,
			status != vm.StatusNotFound,
			bootstrapAge,
			bootstrapThreshold,
			vmAge,
			vmThreshold,
		)
		switch choice {
		case CombinedFullBootstrap:
			return svc.fullBootstrap(ctx)
		case CombinedFastRecreate:
			return svc.fastRecreate(ctx)
		case CombinedContinue:
			return svc.resumeOrStartVM(ctx, status)
		default:
			return svc.resumeOrStartVM(ctx, status)
		}
	default:
		return nil
	}
}

func runtimeLabel(backend, vmType string) string {
	if backend == "" {
		backend = "lima"
	}
	if backend == "docker" {
		return "docker"
	}
	if vmType == "" {
		return backend
	}
	return fmt.Sprintf("%s/%s", backend, vmType)
}

func RuntimeChangedForTest(svc *LifecycleService, state *BootstrapState) bool {
	return svc.runtimeChanged(state)
}
