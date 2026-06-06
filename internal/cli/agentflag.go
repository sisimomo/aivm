package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	agentFlag    string
	agentFlagSet bool
)

func recordAgentFlag(cmd *cobra.Command) {
	agentFlagSet = false
	agentFlag = ""

	root := cmd.Root()
	if root.Flags().Changed("agent") {
		agentFlagSet = true
		agentFlag, _ = root.Flags().GetString("agent")
		return
	}

	if v, ok := AgentFromArgs(os.Args[1:]); ok {
		agentFlagSet = true
		agentFlag = v
	}
}

// effectiveAgent returns the agent override from cobra or os.Args fallback.
func effectiveAgent() string {
	if agentFlagSet {
		return agentFlag
	}
	return ""
}

// AgentFromArgs finds --agent before the first "--" (agent arg separator).
// Needed because cobra does not parse parent persistent flags when the "agent"
// subcommand has DisableFlagParsing enabled.
func AgentFromArgs(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if v, ok := parseAgentArg(args[i]); ok {
			return v, true
		}
		if args[i] == "--agent" &&
			i+1 < len(args) &&
			args[i+1] != "--" &&
			!strings.HasPrefix(args[i+1], "-") {
			return args[i+1], true
		}
	}
	return "", false
}

func parseAgentArg(arg string) (string, bool) {
	const prefix = "--agent="
	if strings.HasPrefix(arg, prefix) {
		return strings.TrimPrefix(arg, prefix), true
	}
	return "", false
}
