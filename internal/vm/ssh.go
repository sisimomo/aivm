package vm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InteractiveSSH opens an interactive SSH session into a Colima VM profile,
// executing script inside the VM. env maps environment variable names to values
// that are injected into the remote shell environment.
func InteractiveSSH(ctx context.Context, profile string, env map[string]string, script string) error {
	envParts := make([]string, 0, len(env))
	for k, v := range env {
		envParts = append(envParts, k+"="+ShellEscape(v))
	}
	bashCmd := "bash -lc " + ShellEscape(script)
	if len(envParts) > 0 {
		bashCmd = strings.Join(envParts, " ") + " " + bashCmd
	}

	home, _ := os.UserHomeDir()
	colimaHome := os.Getenv("COLIMA_HOME")
	if colimaHome == "" {
		colimaHome = filepath.Join(home, ".colima")
	}
	sshConfig := filepath.Join(colimaHome, "_lima", "colima-"+profile, "ssh.config")
	sshHost := "lima-colima-" + profile

	// colima ssh -- CMD runs without a PTY; TUI apps need one, so use ssh -t directly
	// with the SSH config file that colima/lima writes.
	cmd := exec.CommandContext(ctx, "ssh", "-t", "-F", sshConfig, sshHost, bashCmd)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ShellEscape wraps s in single quotes, escaping any embedded single quotes.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
