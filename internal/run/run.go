package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Run executes a command, streaming stdout+stderr to w.
func Run(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// RunEnv executes a command with extra env vars, streaming output to w.
func RunEnv(ctx context.Context, w io.Writer, env map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.Run()
}

// Output runs a command and returns combined output as string.
func Output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Interactive attaches the command directly to the terminal (stdin/stdout/stderr).
func Interactive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InteractiveEnv attaches the command to the terminal with extra env vars.
func InteractiveEnv(ctx context.Context, env map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.Run()
}

// Check returns true if the named binary exists in PATH.
func Check(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// OutputLines runs a command and returns non-empty lines.
func OutputLines(ctx context.Context, name string, args ...string) ([]string, error) {
	out, err := Output(ctx, name, args...)
	if err != nil || out == "" {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// Quiet discards output, returns only error.
func Quiet(ctx context.Context, name string, args ...string) error {
	_, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return err
}

// Buffer captures combined output into a bytes.Buffer.
func Buffer(ctx context.Context, name string, args ...string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	return &buf, cmd.Run()
}
