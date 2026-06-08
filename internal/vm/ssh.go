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

// LimaSSHEndpoint returns the ssh/scp config file path and SSH hostname for a
// Lima instance. Written by limactl at VM creation time.
func LimaSSHEndpoint(profile string) (sshConfig, sshHost string) {
	home, _ := os.UserHomeDir()
	limaHome := os.Getenv("LIMA_HOME")
	if limaHome == "" {
		limaHome = filepath.Join(home, ".lima")
	}
	sshConfig = filepath.Join(limaHome, profile, "ssh.config")
	sshHost = "lima-" + profile
	return
}

func colimaSSHEndpoint(profile string) (string, string) {
	return LimaSSHEndpoint(profile)
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
func NewQuietStderr(dst io.Writer) *quietStderr {
	return &quietStderr{dst: dst}
}

func attachProcessStderr(cmd *exec.Cmd) func() {
	if aivmlog.ToolMode() {
		q := NewQuietStderr(os.Stderr)
		cmd.Stderr = q
		return q.Flush
	}
	cmd.Stderr = os.Stderr
	return func() {}
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

// Flush writes any buffered stderr bytes that were not newline-terminated.
func (q *quietStderr) Flush() {
	if len(q.buf) == 0 {
		return
	}
	line := q.buf
	q.buf = nil
	if quietSSHLine(line) {
		return
	}
	_, _ = q.dst.Write(line)
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

	sshConfig, sshHost := LimaSSHEndpoint(profile)

	// colima ssh -- CMD runs without a PTY; TUI apps need one, so use ssh -t directly
	// with the SSH config file that colima/lima writes.
	args := []string{"-t", "-F", sshConfig}
	args = append(args, OpenSSHOptions()...)
	args = append(args, sshHost, bashCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	flush := attachProcessStderr(cmd)
	defer flush()
	return cmd.Run()
}

// ShellEscape wraps s in single quotes, escaping any embedded single quotes.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
