package log

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const (
	maxSubprocessBufBytes             = 256 * 1024 // 256 KiB per partial line
	logFileMode           os.FileMode = 0600
)

var maxLogFileBytes int64 = 10 * 1024 * 1024 // 10 MiB

// InitStateDir opens the log directory under stateDir and attaches aivm.log.
func InitStateDir(stateDir string) error {
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	path := filepath.Join(logDir, "aivm.log")
	f, err := openLogFile(path)
	if err != nil {
		return fmt.Errorf("opening aivm.log: %w", err)
	}
	attachLogFile(path, f)
	return nil
}

// UseDedicatedLog redirects slog file output for the current process.
// Used by the idle monitor daemon (idle-monitor.log).
func UseDedicatedLog(stateDir, name string) error {
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}
	path := filepath.Join(logDir, name+".log")
	f, err := openLogFile(path)
	if err != nil {
		return fmt.Errorf("opening %s.log: %w", name, err)
	}
	attachLogFile(path, f)
	return nil
}

func openLogFile(path string) (*os.File, error) {
	if err := rotateIfNeeded(path); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logFileMode)
}

func rotateIfNeeded(path string) error {
	return withLogFileLock(path, func() error {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Size() < maxLogFileBytes {
			return nil
		}
		backup := path + ".1"
		_ = os.Remove(backup)
		return os.Rename(path, backup)
	})
}

// withLogFileLock serializes rotation across concurrent aivm processes.
func withLogFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, logFileMode)
	if err != nil {
		return err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	}()
	return fn()
}

// subprocessWriter tees external command output into aivm.log at trace level
// with a [source] tag. No separate per-source log files.
type subprocessWriter struct {
	mu     sync.Mutex
	source string
	buf    []byte
}

func (w *subprocessWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

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
		line := strings.TrimSpace(string(w.buf[:idx]))
		if line != "" {
			slog.Log(context.Background(), SlogTrace, line, slog.String("component", w.source))
		}
		w.buf = w.buf[idx+1:]
	}
	if len(w.buf) > maxSubprocessBufBytes {
		line := strings.TrimSpace(string(w.buf))
		w.buf = nil
		if line != "" {
			slog.Log(context.Background(), SlogTrace, line, slog.String("component", w.source))
		}
	}
	return len(p), nil
}

// Close flushes any trailing partial line buffered from subprocess output.
func (w *subprocessWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	line := strings.TrimSpace(string(w.buf))
	w.buf = nil
	if line != "" {
		slog.Log(context.Background(), SlogTrace, line, slog.String("component", w.source))
	}
	return nil
}
