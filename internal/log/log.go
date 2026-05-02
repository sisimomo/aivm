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

func Info(msg string, args ...any) {
	fmt.Fprintf(os.Stdout, "%s %s %sINFO%s  %s\n", prefix(), ts(), colorGreen, colorReset, fmt.Sprintf(msg, args...))
}
func Success(msg string, args ...any) {
	fmt.Fprintf(os.Stdout, "%s %s %s✓%s     %s\n", prefix(), ts(), colorGreen, colorReset, fmt.Sprintf(msg, args...))
}
func Warn(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s %sWARN%s  %s\n", prefix(), ts(), colorYellow, colorReset, fmt.Sprintf(msg, args...))
}
func Error(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s %sERROR%s %s\n", prefix(), ts(), colorRed, colorReset, fmt.Sprintf(msg, args...))
}
func Step(msg string, args ...any) {
	fmt.Fprintf(os.Stdout, "\n%s %s %s────%s %s %s────%s\n", prefix(), ts(), colorCyan, colorReset, fmt.Sprintf(msg, args...), colorCyan, colorReset)
}
func Debug(msg string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stdout, "%s %s %sDEBUG%s %s\n", prefix(), ts(), colorCyan, colorReset, fmt.Sprintf(msg, args...))
	}
}
func Fatal(msg string, args ...any) {
	Error("FATAL: "+msg, args...)
	os.Exit(1)
}

// Writer returns an io.Writer that prefixes lines with the plugin name.
func Writer(pluginName string) io.Writer {
	return &prefixWriter{prefix: "[" + pluginName + "] "}
}

type prefixWriter struct {
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
		fmt.Fprintf(os.Stdout, "%s%s", w.prefix, line)
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
}
