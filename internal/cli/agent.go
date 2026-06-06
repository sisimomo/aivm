package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
)

func AgentCmd(getApp func() (*App, error), _ *string) *cobra.Command {
	return &cobra.Command{
		Use:                "agent",
		DisableFlagParsing: true, // forward -p, --version, etc. to the agent CLI after "--"
		Short:              "Run the agent CLI in the VM with custom arguments",
		Long: `Run the configured agent CLI inside the VM, forwarding arguments verbatim.

Use '--' to separate aivm flags from agent flags:

  aivm agent -- -p "Refactor utils.js to use arrow functions"
  aivm --agent cursor agent -- -p "fix the tests"

Interactive shortcut (uses launch_args from config):

  aivm
  aivm --agent claude`,
		Example: `  aivm agent -- -p "Refactor all functions in utils.js to use arrow functions"
  aivm --agent cursor agent -- --version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentArgs, err := agentArgsAfterDash(cmd, args)
			if err != nil {
				return err
			}

			app, err := getApp()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if err := DoStart(ctx, app); err != nil {
				return err
			}
			return DoAgent(ctx, app, effectiveAgent(), agentArgs)
		},
	}
}

func DoAgent(ctx context.Context, app *App, agentOverride string, args []string) error {
	return app.Lifecycle.AgentRun(ctx, agentOverride, args)
}

// agentArgsAfterDash returns arguments after '--'. With DisableFlagParsing, cobra
// does not always set ArgsLenAtDash, so we also accept a literal "--" in args.
func agentArgsAfterDash(cmd *cobra.Command, args []string) ([]string, error) {
	const msgMissing = "missing '--' before agent arguments\n\nUse: aivm agent -- <agent args>\nExample: aivm agent -- -p \"refactor utils.js\""

	if idx := cmd.ArgsLenAtDash(); idx >= 0 {
		agentArgs := args[idx:]
		if len(agentArgs) == 0 {
			return nil, errors.New("no agent arguments after '--'")
		}
		return agentArgs, nil
	}
	for i, a := range args {
		if a == "--" {
			agentArgs := args[i+1:]
			if len(agentArgs) == 0 {
				return nil, errors.New("no agent arguments after '--'")
			}
			return agentArgs, nil
		}
	}
	return nil, errors.New(msgMissing)
}
