package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"
)

var (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// logMode controls how log messages are rendered.
type logMode int

const (
	// ModeFull is verbose mode: all levels print, plugin output streams.
	// Used by test harnesses and when --debug / debug:true is set.
	ModeFull logMode = iota
	// ModeClean suppresses Info/Debug and discards plugin output.
	// Steps are printed as a simple numbered sequence.
	ModeClean
)

var debug bool

// SetDebug enables debug output. It also switches Default to ModeFull so that
// all Info/Step/Debug messages are printed (current verbose behaviour).
func SetDebug(v bool) {
	debug = v
	if v {
		Default.mode = ModeFull
	}
}

func ts() string     { return time.Now().Format("15:04:05") }
func prefix() string { return colorBold + colorBlue + "[aivm]" + colorReset }

// Logger is a named writer pair for stdout and stderr.
// Construct with New (verbose, for tests) or use Default (clean mode).
type Logger struct {
	Out io.Writer // receives Info, Success, Step, Debug messages
	Err io.Writer // receives Warn, Error messages

	mode  logMode
	stepN int
}

// New returns a ModeFull (verbose) Logger. Use in tests so all messages are
// captured and assertions continue to work without change.
func New(out, err io.Writer) *Logger {
	return &Logger{Out: out, Err: err, mode: ModeFull}
}

// Default is the logger used by the package-level functions.
var Default = &Logger{Out: os.Stdout, Err: os.Stderr, mode: ModeClean}

func (l *Logger) Info(msg string, args ...any) {
	if l.mode == ModeFull {
		fmt.Fprintf(l.Out, "%s %s %sINFO%s  %s\n", prefix(), ts(), colorGreen, colorReset, fmt.Sprintf(msg, args...))
	}
}

func (l *Logger) Success(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if l.mode == ModeFull {
		fmt.Fprintf(l.Out, "%s %s %s✓%s     %s\n", prefix(), ts(), colorGreen, colorReset, text)
	} else {
		fmt.Fprintf(l.Out, "%s✓%s  %s\n", colorGreen, colorReset, text)
	}
}

func (l *Logger) Warn(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if l.mode == ModeFull {
		fmt.Fprintf(l.Err, "%s %s %sWARN%s  %s\n", prefix(), ts(), colorYellow, colorReset, text)
	} else {
		fmt.Fprintf(l.Err, "%s⚠%s  %s\n", colorYellow, colorReset, text)
	}
}

func (l *Logger) Error(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if l.mode == ModeFull {
		fmt.Fprintf(l.Err, "%s %s %sERROR%s %s\n", prefix(), ts(), colorRed, colorReset, text)
	} else {
		fmt.Fprintf(l.Err, "%s✗%s  %s\n", colorRed, colorReset, text)
	}
}

func (l *Logger) Step(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if l.mode == ModeFull {
		fmt.Fprintf(l.Out, "\n%s %s %s────%s %s %s────%s\n", prefix(), ts(), colorCyan, colorReset, text, colorCyan, colorReset)
	} else {
		l.stepN++
		fmt.Fprintf(l.Out, "[%d] %s...\n", l.stepN, text)
	}
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
// writing to l.Out. In ModeClean, plugin output is discarded.
func (l *Logger) Writer(pluginName string) io.Writer {
	if l.mode == ModeClean {
		return io.Discard
	}
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
	if os.Getenv("NO_COLOR") != "" {
		colorReset = ""
		colorBlue = ""
		colorGreen = ""
		colorYellow = ""
		colorRed = ""
		colorCyan = ""
		colorBold = ""
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
}
