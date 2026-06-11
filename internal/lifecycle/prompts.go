package lifecycle

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
)

type CombinedChoice int

const (
	CombinedFullBootstrap CombinedChoice = iota + 1
	CombinedFastRecreate
	CombinedContinue
)

func PromptBootstrapRefresh(out io.Writer, c Confirmer, bootstrapAge, threshold time.Duration) bool {
	bootstrapDays := int(bootstrapAge.Hours() / 24)
	thresholdDays := int(threshold.Hours() / 24)
	slog.Warn(fmt.Sprintf("Bootstrap is %d day(s) old (threshold: %d days)", bootstrapDays, thresholdDays))
	return PromptYesNo(out, c, "  → Rerun full bootstrap to update toolchains? [y/N] ", false)
}

func PromptCombined(out io.Writer, c Confirmer, vmExists bool, bootstrapAge, bootstrapThreshold, vmAge, vmThreshold time.Duration) CombinedChoice {
	bootstrapDays := int(bootstrapAge.Hours() / 24)
	bootstrapThresholdDays := int(bootstrapThreshold.Hours() / 24)
	vmDays := int(vmAge.Hours() / 24)
	vmThresholdDays := int(vmThreshold.Hours() / 24)

	slog.Warn(fmt.Sprintf("Bootstrap is %d day(s) old (threshold: %d days)", bootstrapDays, bootstrapThresholdDays))
	slog.Warn(fmt.Sprintf("VM is %d day(s) old (threshold: %d days)", vmDays, vmThresholdDays))

	fmt.Fprintln(out, "  → [1] Rerun full bootstrap to update toolchains")
	fmt.Fprintln(out, "  → [2] Fast recreate from base image")
	if vmExists {
		fmt.Fprintln(out, "  → [3] Continue without changes")
	}
	fmt.Fprint(out, "  → Choose an option: ")

	switch strings.TrimSpace(c.ReadAnswer()) {
	case "1":
		return CombinedFullBootstrap
	case "2":
		return CombinedFastRecreate
	case "3":
		if vmExists {
			return CombinedContinue
		}
	}

	if vmExists {
		return CombinedContinue
	}
	return CombinedFastRecreate
}

func PromptRuntimeChange(out io.Writer, c Confirmer, from, to string) bool {
	slog.Warn(fmt.Sprintf("VM runtime has changed (%s → %s)", from, to))
	return PromptYesNo(out, c, "  → This will destroy the VM and base image. Continue? [y/N] ", false)
}
