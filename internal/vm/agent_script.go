package vm

import (
	"fmt"
	"strings"
)

// BuildLaunchScript returns a bash script that cds to workDir and execs the agent
// with configured launch_args (interactive shortcut).
func BuildLaunchScript(workDir, cliCommand, launchArgs string) string {
	return buildAgentSessionScript(workDir, agentExecLine(cliCommand, launchArgs, nil))
}

// BuildRunScript returns a bash script that cds to workDir and execs the agent
// with the given user-supplied arguments (aivm agent -- …).
func BuildRunScript(workDir, cliCommand string, args []string) string {
	return buildAgentSessionScript(workDir, agentExecLine(cliCommand, "", args))
}

func buildAgentSessionScript(workDir, execLine string) string {
	return fmt.Sprintf(`
set -e
if [[ ! -d %s ]]; then
  echo "[aivm] ERROR: VM directory %s does not exist"
  exit 1
fi
cd %s
%s
`, ShellEscape(workDir), ShellEscape(workDir), ShellEscape(workDir), execLine)
}

func agentExecLine(cliCommand, launchArgs string, args []string) string {
	parts := []string{"exec", ShellEscape(cliCommand)}
	if len(args) > 0 {
		for _, a := range args {
			parts = append(parts, ShellEscape(a))
		}
	} else if launchArgs != "" {
		parts = append(parts, launchArgs)
	}
	return strings.Join(parts, " ")
}
