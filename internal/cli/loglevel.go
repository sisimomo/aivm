package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	aivmlog "github.com/sisimomo/aivm/internal/log"
)

var (
	logLevelFlag string
	logLevelSet  bool
)

func recordLogLevelFlag(cmd *cobra.Command) {
	logLevelSet = false
	logLevelFlag = ""

	root := cmd.Root()
	if root.Flags().Changed("log-level") {
		logLevelSet = true
		logLevelFlag, _ = root.Flags().GetString("log-level")
		return
	}

	if v, ok := LogLevelFromArgs(os.Args[1:]); ok {
		logLevelSet = true
		logLevelFlag = v
	}
}

// LogLevelFromArgs finds --log-level before the first "--" (agent arg separator).
func LogLevelFromArgs(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if v, ok := parseLogLevelArg(args[i]); ok {
			return v, true
		}
		if args[i] == "--log-level" &&
			i+1 < len(args) &&
			args[i+1] != "--" &&
			!strings.HasPrefix(args[i+1], "-") {
			return args[i+1], true
		}
	}
	return "", false
}

func parseLogLevelArg(arg string) (string, bool) {
	const prefix = "--log-level="
	if strings.HasPrefix(arg, prefix) {
		return strings.TrimPrefix(arg, prefix), true
	}
	return "", false
}

// ApplyLogLevel resolves and applies the effective log level (flag → env → config).
func ApplyLogLevel(cfgLevel string) error {
	level, err := aivmlog.ResolveLevel(logLevelFlag, logLevelSet, cfgLevel)
	if err != nil {
		return err
	}
	aivmlog.SetLevel(level)
	return nil
}
