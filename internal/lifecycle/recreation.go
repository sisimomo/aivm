package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sisimomo/aivm/internal/config"
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
		if interactive && (timers.BootstrapDue || timers.VMAgeDue) {
			return ActionPromptCombined, nil
		}
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
	case ActionPromptRuntimeChange, ActionPromptConfigChange:
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
	case ActionPromptBootstrapRefresh, ActionPromptCombined:
		choice := strings.ToLower(strings.TrimSpace(svc.Confirmer.ReadAnswer()))
		if choice == "y" || choice == "1" {
			return svc.fullBootstrap(ctx)
		}
		return svc.fastRecreate(ctx)
	default:
		return nil
	}
}

func RuntimeChangedForTest(svc *LifecycleService, state *BootstrapState) bool {
	return svc.runtimeChanged(state)
}
