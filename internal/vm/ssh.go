package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	aivmlog "github.com/sisimomo/aivm/internal/log"
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

// OpenSSHOptions returns extra -o flags for direct ssh when aivm is in tool mode (log level error).
func OpenSSHOptions() []string {
	if !aivmlog.ToolMode() {
		return nil
	}
	// LogLevel=ERROR hides "Shared connection to … closed" (ControlMaster teardown).
	// ControlMaster=no avoids multiplexed-session noise for one-shot agent runs.
	return []string{
		"-o", "LogLevel=ERROR",
		"-o", "ControlMaster=no",
	}
}

// NewQuietStderr wraps dst and drops benign OpenSSH teardown lines.
func NewQuietStderr(dst io.Writer) io.Writer {
	return &quietStderr{dst: dst}
}

func attachProcessStderr(cmd *exec.Cmd) {
	if aivmlog.ToolMode() {
		cmd.Stderr = NewQuietStderr(os.Stderr)
		return
	}
	cmd.Stderr = os.Stderr
}

// IsBenignSSHStderrLine reports whether line is non-failure SSH noise (e.g. ControlMaster teardown).
func IsBenignSSHStderrLine(line string) bool {
	return quietSSHLine([]byte(line))
}

// quietStderr drops benign OpenSSH info lines that slip through at LogLevel=ERROR.
type quietStderr struct {
	dst io.Writer
	buf []byte
}

func (q *quietStderr) Write(p []byte) (int, error) {
	q.buf = append(q.buf, p...)
	for {
		i := bytes.IndexByte(q.buf, '\n')
		if i < 0 {
			break
		}
		line := q.buf[:i]
		q.buf = q.buf[i+1:]
		if quietSSHLine(line) {
			continue
		}
		if _, err := q.dst.Write(append(line, '\n')); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func quietSSHLine(line []byte) bool {
	s := strings.TrimSpace(string(line))
	if s == "" {
		return true
	}
	return strings.HasPrefix(s, "Shared connection to ") && strings.HasSuffix(s, " closed.")
}

// InteractiveSSH opens an interactive SSH session into a Colima VM profile,
// executing script inside the VM. env maps environment variable names to values
// that are injected into the remote shell environment.
func InteractiveSSH(ctx context.Context, profile string, env map[string]string, script string) error {
	bashCmd := "bash -lc " + ShellEscape(SSHScriptWithEnv(env, script))

	sshConfig, sshHost := colimaSSHEndpoint(profile)

	// colima ssh -- CMD runs without a PTY; TUI apps need one, so use ssh -t directly
	// with the SSH config file that colima/lima writes.
	args := []string{"-t", "-F", sshConfig}
	args = append(args, OpenSSHOptions()...)
	args = append(args, sshHost, bashCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	attachProcessStderr(cmd)
	return cmd.Run()
}

// ShellEscape wraps s in single quotes, escaping any embedded single quotes.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
