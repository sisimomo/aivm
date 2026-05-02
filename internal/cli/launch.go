package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	aivmlog "aivm/internal/log"
	"aivm/internal/vm"
)

func LaunchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "launch [directory]",
		Short: "Launch Claude Code in the VM (default command)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return DoLaunch(cmd.Context(), app)
		},
	}
}

func DoLaunch(ctx context.Context, app *App) error {
	cfg := app.Config

	token := cfg.Auth.ClaudeToken
	if token == "" {
		return fmt.Errorf("auth.claude_token is not set — edit aivm.yaml or set AIVM_AUTH_CLAUDE_TOKEN")
	}

	hostCWD, _ := os.Getwd()
	devRoot := cfg.VM.DevRoot
	realCWD, _ := filepath.EvalSymlinks(hostCWD)
	realDev, _ := filepath.EvalSymlinks(devRoot)

	if !strings.HasPrefix(realCWD, realDev) {
		return fmt.Errorf("current directory '%s' is not under AIVM_DEV_ROOT (%s)\naivm only works inside %s", realCWD, devRoot, devRoot)
	}

	status, err := app.VM.Status(ctx)
	if err != nil || status != vm.StatusRunning {
		return fmt.Errorf("VM is not running — run 'aivm start' first")
	}

	sess, err := app.Sessions.Create(hostCWD)
	if err != nil {
		aivmlog.Warn("could not create session lock: %v", err)
	} else {
		defer sess.Remove()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		if sess != nil {
			sess.Remove()
		}
		os.Exit(0)
	}()

	vmDir := realCWD

	aivmlog.Info("Host: %s", hostCWD)
	aivmlog.Info("VM:   %s", vmDir)
	aivmlog.Step("Launching Claude Code in VM")

	script := fmt.Sprintf(`
set -e
export PATH="$HOME/.claude/local/bin:$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"
if [[ ! -d %s ]]; then
  echo "[aivm] ERROR: VM directory %s does not exist"
  exit 1
fi
cd %s
exec claude --dangerously-skip-permissions --mcp-config "$HOME/.claude/mcp-config.json"
`, shellescape(vmDir), shellescape(vmDir), shellescape(vmDir))

	colimaVM, ok := app.VM.(*vm.ColimaVM)
	if !ok {
		return fmt.Errorf("VM implementation does not support interactive claude launch")
	}

	return interactiveSsh(ctx, colimaVM.Profile(), map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": token,
	}, script)
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func interactiveSsh(ctx context.Context, profile string, env map[string]string, script string) error {
	envParts := []string{}
	for k, v := range env {
		envParts = append(envParts, k+"="+shellescape(v))
	}
	bashCmd := "bash -lc " + shellescape(script)
	if len(envParts) > 0 {
		bashCmd = strings.Join(envParts, " ") + " " + bashCmd
	}

	// colima ssh -- CMD runs without a PTY; TUI apps like claude need one.
	// Use ssh -t directly with the SSH config file that colima/lima writes.
	// The config is at $COLIMA_HOME/_lima/colima-<profile>/ssh.config and the
	// hostname inside it is lima-colima-<profile>.
	home, _ := os.UserHomeDir()
	colimaHome := os.Getenv("COLIMA_HOME")
	if colimaHome == "" {
		colimaHome = filepath.Join(home, ".colima")
	}
	sshConfig := filepath.Join(colimaHome, "_lima", "colima-"+profile, "ssh.config")
	sshHost := "lima-colima-" + profile

	cmd := exec.CommandContext(ctx, "ssh", "-t", "-F", sshConfig, sshHost, bashCmd)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
