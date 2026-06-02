package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// colimaSSHEndpoint returns the scp/ssh config file path and the SSH hostname
// for a given Colima profile. These are written by colima/lima at VM creation
// time and consumed by scp/ssh directly (bypassing the `colima ssh` wrapper).
func colimaSSHEndpoint(profile string) (sshConfig, sshHost string) {
	home, _ := os.UserHomeDir()
	colimaHome := os.Getenv("COLIMA_HOME")
	if colimaHome == "" {
		colimaHome = filepath.Join(home, ".colima")
	}
	sshConfig = filepath.Join(colimaHome, "_lima", "colima-"+profile, "ssh.config")
	sshHost = "lima-colima-" + profile
	return
}

// SSHScriptWithEnv prepends export statements so session variables are visible
// inside the remote login shell.
func SSHScriptWithEnv(env map[string]string, script string) string {
	if len(env) == 0 {
		return script
	}
	var exports strings.Builder
	for k, v := range env {
		fmt.Fprintf(&exports, "export %s=%s; ", k, ShellEscape(v))
	}
	return exports.String() + script
}

// InteractiveSSH opens an interactive SSH session into a Colima VM profile,
// executing script inside the VM. env maps environment variable names to values
// that are injected into the remote shell environment.
func InteractiveSSH(ctx context.Context, profile string, env map[string]string, script string) error {
	bashCmd := "bash -lc " + ShellEscape(SSHScriptWithEnv(env, script))

	sshConfig, sshHost := colimaSSHEndpoint(profile)

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
