package lifecycle

import (
	"fmt"
	"io"
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
// Returns vmAgeRecreate only when the user answers y/Y.
func promptVMAge(l *aivmlog.Logger, c Confirmer, profile string, age, threshold time.Duration) vmAgeDecision {
	if !c.IsInteractive() {
		l.Info("VM is %s old but running non-interactively — keeping", age.Round(time.Hour))
		return vmAgeKeep
	}
	ageDays := int(age.Hours() / 24)
	thresholdDays := int(threshold.Hours() / 24)
	l.Warn("VM '%s' is %d day(s) old (threshold: %d days)", profile, ageDays, thresholdDays)
	fmt.Fprintf(l.Out, "  → Delete and recreate for a clean slate? [y/N] ")
	answer := c.ReadAnswer()
	if answer == "y" || answer == "Y" {
		return vmAgeRecreate
	}
	return vmAgeKeep
}

// promptYesNo prints the prompt string and returns true only for y/Y.
func promptYesNo(out io.Writer, c Confirmer, prompt string) bool {
	fmt.Fprint(out, prompt)
	ans := c.ReadAnswer()
	return ans == "y" || ans == "Y"
}

// promptConfigChanged asks the user whether to recreate the VM to apply
// pending config changes. Returns true only when the user answers y/Y.
func promptConfigChanged(out io.Writer, c Confirmer) bool {
	fmt.Fprintf(out, "  → Recreate VM to apply changes? [y/N] ")
	ans := c.ReadAnswer()
	return ans == "y" || ans == "Y"
}
