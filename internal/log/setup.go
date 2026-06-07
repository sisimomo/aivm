package log

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
)

var (
	setupMu  sync.Mutex
	defaultH *handler
	termOut  io.Writer = os.Stdout
)

func init() {
	defaultH = newHandler(os.Stdout, os.Stderr, LevelInfo)
	slog.SetDefault(slog.New(defaultH))
}

// Configure replaces the global logger's terminal sinks and level.
// An attached log file sink is preserved.
func Configure(out, err io.Writer, level Level) {
	setupMu.Lock()
	defer setupMu.Unlock()

	var file io.Writer
	var filePath string
	if defaultH != nil {
		st := defaultH.state
		st.writeMu.Lock()
		file = st.file
		filePath = st.filePath
		st.writeMu.Unlock()
	}

	defaultH = newHandler(out, err, level)
	if filePath != "" {
		if f, ok := file.(*os.File); ok {
			defaultH.attachFile(filePath, f)
		}
	}
	termOut = out
	slog.SetDefault(slog.New(defaultH))
}

func withHandler(fn func(*handler)) {
	setupMu.Lock()
	defer setupMu.Unlock()
	fn(defaultH)
}

// SetLevel configures terminal verbosity.
func SetLevel(l Level) {
	withHandler(func(h *handler) {
		if h != nil {
			h.setTerminalLevel(l)
		}
	})
}

// GetLevel returns the current terminal log level.
func GetLevel() Level {
	setupMu.Lock()
	defer setupMu.Unlock()
	if defaultH == nil {
		return LevelInfo
	}
	return defaultH.terminalLevel()
}

// ToolMode is true at log level error — suppress non-failure noise from aivm and subprocesses.
func ToolMode() bool {
	return GetLevel() == LevelError
}

// TerminalOut is stdout for interactive prompts (not subject to log level).
func TerminalOut() io.Writer {
	setupMu.Lock()
	defer setupMu.Unlock()
	return termOut
}

// Writer captures subprocess stdout/stderr into aivm.log at trace (tagged [source]).
// Terminal shows these lines only at --log-level trace.
// Prefer RunCmd or WithWriter so buffered output is flushed automatically.
func Writer(source string) io.Writer {
	return &subprocessWriter{source: source}
}

// RunCmd runs cmd with stdout and stderr captured into the unified log for source.
func RunCmd(cmd *exec.Cmd, source string) error {
	w := &subprocessWriter{source: source}
	defer w.Close()
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

// WithWriter captures output written during fn into the unified log for source.
func WithWriter(source string, fn func(io.Writer) error) error {
	w := &subprocessWriter{source: source}
	defer w.Close()
	return fn(w)
}

func attachLogFile(path string, f *os.File) {
	withHandler(func(h *handler) {
		if h != nil {
			h.attachFile(path, f)
		}
	})
}
