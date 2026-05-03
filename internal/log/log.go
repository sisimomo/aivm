package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

const (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

var debug bool

func SetDebug(v bool) { debug = v }

func ts() string     { return time.Now().Format("15:04:05") }
func prefix() string { return colorBold + colorBlue + "[aivm]" + colorReset }

// Logger is a named writer pair for stdout and stderr. The zero value is not
// usable; construct one with New or use Default.
type Logger struct {
	Out io.Writer // receives Info, Success, Step, Debug messages
	Err io.Writer // receives Warn, Error messages
}

// New returns a Logger that writes to out (stdout) and err (stderr).
func New(out, err io.Writer) *Logger { return &Logger{Out: out, Err: err} }

// Default is the logger used by the package-level functions.
var Default = &Logger{Out: os.Stdout, Err: os.Stderr}

func (l *Logger) Info(msg string, args ...any) {
	fmt.Fprintf(l.Out, "%s %s %sINFO%s  %s\n", prefix(), ts(), colorGreen, colorReset, fmt.Sprintf(msg, args...))
}
func (l *Logger) Success(msg string, args ...any) {
	fmt.Fprintf(l.Out, "%s %s %s✓%s     %s\n", prefix(), ts(), colorGreen, colorReset, fmt.Sprintf(msg, args...))
}
func (l *Logger) Warn(msg string, args ...any) {
	fmt.Fprintf(l.Err, "%s %s %sWARN%s  %s\n", prefix(), ts(), colorYellow, colorReset, fmt.Sprintf(msg, args...))
}
func (l *Logger) Error(msg string, args ...any) {
	fmt.Fprintf(l.Err, "%s %s %sERROR%s %s\n", prefix(), ts(), colorRed, colorReset, fmt.Sprintf(msg, args...))
}
func (l *Logger) Step(msg string, args ...any) {
	fmt.Fprintf(l.Out, "\n%s %s %s────%s %s %s────%s\n", prefix(), ts(), colorCyan, colorReset, fmt.Sprintf(msg, args...), colorCyan, colorReset)
}
func (l *Logger) Debug(msg string, args ...any) {
	if debug {
		fmt.Fprintf(l.Out, "%s %s %sDEBUG%s %s\n", prefix(), ts(), colorCyan, colorReset, fmt.Sprintf(msg, args...))
	}
}

// Package-level functions delegate to Default for backward compatibility.

func Info(msg string, args ...any)    { Default.Info(msg, args...) }
func Success(msg string, args ...any) { Default.Success(msg, args...) }
func Warn(msg string, args ...any)    { Default.Warn(msg, args...) }
func Error(msg string, args ...any)   { Default.Error(msg, args...) }
func Step(msg string, args ...any)    { Default.Step(msg, args...) }
func Debug(msg string, args ...any)   { Default.Debug(msg, args...) }

func Fatal(msg string, args ...any) {
	Error("FATAL: "+msg, args...)
	os.Exit(1)
}

// Writer returns an io.Writer that prefixes lines with the plugin name,
// writing to l.Out.
func (l *Logger) Writer(pluginName string) io.Writer {
	return &prefixWriter{out: l.Out, prefix: "[" + pluginName + "] "}
}

// Writer returns an io.Writer that prefixes lines with the plugin name.
func Writer(pluginName string) io.Writer {
	return Default.Writer(pluginName)
}

type prefixWriter struct {
	out    io.Writer
	prefix string
	buf    []byte
}

func (w *prefixWriter) Write(p []byte) (n int, err error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := w.buf[:idx+1]
		fmt.Fprintf(w.out, "%s%s", w.prefix, line)
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
}
