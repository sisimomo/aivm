package lifecycle

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

// vmAgeDecision is the user's answer to whether to recreate an aged VM.
type vmAgeDecision bool

const (
	vmAgeKeep     vmAgeDecision = false
	vmAgeRecreate vmAgeDecision = true
)

// promptVMAge shows the VM age warning and asks whether to recreate.
// Non-interactive: returns vmAgeKeep (default No).
func promptVMAge(c Confirmer, profile string, age, threshold time.Duration) vmAgeDecision {
	if !c.IsInteractive() {
		return vmAgeKeep
	}
	ageDays := int(age.Hours() / 24)
	thresholdDays := int(threshold.Hours() / 24)
	slog.Warn(fmt.Sprintf("VM '%s' is %d day(s) old (threshold: %d days)", profile, ageDays, thresholdDays))
	if PromptYesNo(aivmlog.TerminalOut(), c, "  → Delete and recreate for a clean slate? [y/N] ", false) {
		return vmAgeRecreate
	}
	return vmAgeKeep
}

// PromptYesNo prints prompt when interactive and returns true only for y/Y.
// Non-interactive: returns defaultYes without reading stdin.
func PromptYesNo(out io.Writer, c Confirmer, prompt string, defaultYes bool) bool {
	if !c.IsInteractive() {
		return defaultYes
	}
	fmt.Fprint(out, prompt)
	ans := c.ReadAnswer()
	return ans == "y" || ans == "Y"
}

// PromptConfigChanged asks whether to recreate the VM to apply pending config changes.
// Non-interactive: default No (continue without applying).
func PromptConfigChanged(out io.Writer, c Confirmer) bool {
	return PromptYesNo(out, c, "  → Recreate VM to apply changes? [y/N] ", false)
}
