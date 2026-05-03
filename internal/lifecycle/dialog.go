package lifecycle

import (
	"fmt"
	"io"

	aivmlog "aivm/internal/log"
)

// vmAgeDecision is the user's answer to whether to recreate an aged VM.
type vmAgeDecision bool

const (
	vmAgeKeep     vmAgeDecision = false
	vmAgeRecreate vmAgeDecision = true
)

// promptVMAge shows the VM age warning and asks whether to recreate.
// Returns vmAgeRecreate only when the user answers y/Y.
func promptVMAge(l *aivmlog.Logger, c Confirmer, profile string, ageDays, threshold int) vmAgeDecision {
	if !c.IsInteractive() {
		l.Info("VM is %d days old but running non-interactively — keeping", ageDays)
		return vmAgeKeep
	}
	l.Warn("VM '%s' is %d day(s) old (threshold: %d)", profile, ageDays, threshold)
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

// baseImageRebuildDecision is the user's choice when the base image is stale
// and sessions are active.
type baseImageRebuildDecision int

const (
	baseImageSkip       baseImageRebuildDecision = iota
	baseImageRebuildNow                          // kill sessions and rebuild in place
	baseImageTransition                          // start new parallel VM
)

// promptBaseImageRebuildWithSessions asks the user to pick between killing
// sessions now or transitioning to a parallel VM. nSessions must be > 0.
func promptBaseImageRebuildWithSessions(out io.Writer, c Confirmer, nSessions int) baseImageRebuildDecision {
	fmt.Fprintf(out, "\n  You have %d active session(s).\n", nSessions)
	fmt.Fprintf(out, "  Choose how to proceed:\n")
	fmt.Fprintf(out, "    1. Kill all sessions and rebuild now (sessions will be lost)\n")
	fmt.Fprintf(out, "    2. Start a new VM with the fresh image; old VM runs until sessions end, then auto-deletes\n")
	fmt.Fprintf(out, "  Choice [1/2]: ")
	switch c.ReadAnswer() {
	case "1":
		return baseImageRebuildNow
	case "2":
		return baseImageTransition
	default:
		return baseImageSkip
	}
}

// imageRebuildDecision is the user's choice when `aivm rebuild-image` is run.
type imageRebuildDecision int

const (
	imageRebuildCancel imageRebuildDecision = iota
	imageRebuildHard                        // destroy VM and rebuild in place
	imageRebuildSoft                        // parallel temp VM, old VM drains
)

// promptImageRebuild drives the interactive decision tree for `aivm rebuild-image`
// when force is false. Returns imageRebuildCancel if the user backs out.
func promptImageRebuild(out io.Writer, c Confirmer, nSessions int) imageRebuildDecision {
	if nSessions > 0 {
		fmt.Fprintf(out, "\n  Kill all active sessions now? [y/N] ")
		if ans := c.ReadAnswer(); ans == "y" || ans == "Y" {
			return imageRebuildHard
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "⚠️  A second VM will be created for the rebuild.\n")
		fmt.Fprintf(out, "⚠️  The current VM becomes legacy and will be automatically\n")
		fmt.Fprintf(out, "⚠️  deleted once all its sessions close (no timer).\n")
		fmt.Fprintf(out, "\n  Proceed with soft rebuild? [y/N] ")
		if ans := c.ReadAnswer(); ans == "y" || ans == "Y" {
			return imageRebuildSoft
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
