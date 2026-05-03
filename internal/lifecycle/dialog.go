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

// imageRebuildDecision is the user's choice when `aivm rebuild-image` is run.
type imageRebuildDecision int

const (
	imageRebuildCancel imageRebuildDecision = iota
	imageRebuildHard                        // destroy VM and rebuild in place
)

// promptImageRebuild drives the interactive decision tree for `aivm rebuild-image`
// when force is false. Returns imageRebuildCancel if the user backs out.
func promptImageRebuild(out io.Writer, c Confirmer, nSessions int) imageRebuildDecision {
	if nSessions > 0 {
		fmt.Fprintf(out, "\n  Kill all active sessions now? [y/N] ")
		if ans := c.ReadAnswer(); ans == "y" || ans == "Y" {
			return imageRebuildHard
		}
		fmt.Fprintf(out, "ℹ️  Rebuild cancelled.\n")
		return imageRebuildCancel
	}
	// No active sessions — confirm hard rebuild.
	fmt.Fprintf(out, "\n  Proceed with base image rebuild? [y/N] ")
	if ans := c.ReadAnswer(); ans == "y" || ans == "Y" {
		return imageRebuildHard
	}
	fmt.Fprintf(out, "ℹ️  Rebuild cancelled.\n")
	return imageRebuildCancel
}

// agentMismatchDecision is the user's choice when the VM has a different agent
// installed than the one configured.
type agentMismatchDecision int

const (
	agentMismatchInstall  agentMismatchDecision = iota // add new agent to existing VM
	agentMismatchRecreate                              // destroy VM and recreate
)

// promptAgentMismatch shows the mismatch context and asks the user to choose
// between installing alongside the existing agent or recreating the VM.
// Returns (decision, true) on a valid choice or (0, false) on invalid input.
func promptAgentMismatch(out io.Writer, c Confirmer, installedSummary, configured string, nSessions int) (agentMismatchDecision, bool) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  This VM already has %s installed.\n", installedSummary)
	fmt.Fprintf(out, "  Config now selects %s.\n", configured)
	if nSessions > 0 {
		fmt.Fprintf(out, "  Note: option 2 will terminate %d active session(s).\n", nSessions)
	}
	fmt.Fprintf(out, "  Choose how to proceed:\n")
	fmt.Fprintf(out, "    1. Install %s in the existing VM and keep the current agent(s)\n", configured)
	fmt.Fprintf(out, "    2. Delete the VM and recreate it with only %s\n", configured)
	fmt.Fprintf(out, "  Choice [1/2]: ")
	switch c.ReadAnswer() {
	case "1":
		return agentMismatchInstall, true
	case "2":
		return agentMismatchRecreate, true
	default:
		return 0, false
	}
}
