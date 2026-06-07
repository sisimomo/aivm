package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

type handlerState struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	termLvl  slog.Level
	termOut  io.Writer
	termErr  io.Writer
	file     io.Writer
	filePath string
	fileLvl  slog.Level
	noColor  bool
}

type handler struct {
	state *handlerState
	attrs []slog.Attr
	group string
}

func newHandler(out, err io.Writer, term Level) *handler {
	return &handler{
		state: &handlerState{
			termLvl: term.toSlog(),
			termOut: out,
			termErr: err,
			fileLvl: SlogTrace,
			noColor: os.Getenv("NO_COLOR") != "",
		},
	}
}

func (h *handler) clone() *handler {
	return &handler{
		state: h.state,
		attrs: append([]slog.Attr(nil), h.attrs...),
		group: h.group,
	}
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	st := h.state
	st.mu.Lock()
	termLvl := st.termLvl
	st.mu.Unlock()

	st.writeMu.Lock()
	filePath := st.filePath
	fileLvl := st.fileLvl
	st.writeMu.Unlock()

	if filePath != "" && level >= fileLvl {
		return true
	}
	return level >= termLvl
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "component" {
			msg = fmt.Sprintf("[%s] %s", a.Value.String(), msg)
		} else {
			msg = fmt.Sprintf("%s %s=%v", msg, a.Key, a.Value.Any())
		}
		return true
	})
	for _, a := range h.attrs {
		if a.Key == "component" {
			msg = fmt.Sprintf("[%s] %s", a.Value.String(), msg)
		} else {
			msg = fmt.Sprintf("%s %s=%v", msg, a.Key, a.Value.Any())
		}
	}

	st := h.state
	st.mu.Lock()
	group := h.group
	fileLvl := st.fileLvl
	termLvl := st.termLvl
	termOut := st.termOut
	termErr := st.termErr
	noColor := st.noColor
	st.mu.Unlock()

	if group != "" {
		msg = fmt.Sprintf("[%s] %s", group, msg)
	}

	lvl := slogToLevel(r.Level)

	st.writeMu.Lock()
	defer st.writeMu.Unlock()

	var handleErr error
	if r.Level >= fileLvl && st.filePath != "" {
		if err := st.ensureLogFileReady(); err != nil && handleErr == nil {
			handleErr = err
		}
		if st.file != nil {
			if _, err := fmt.Fprintf(st.file, "%s %s  %s\n", fileTS(), lvl.String(), msg); err != nil && handleErr == nil {
				handleErr = err
			}
		}
	}
	if r.Level >= termLvl {
		if err := writeTerminal(termOut, termErr, lvl, msg, noColor); err != nil && handleErr == nil {
			handleErr = err
		}
	}
	return handleErr
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := h.clone()
	c.attrs = append(c.attrs, attrs...)
	return c
}

func (h *handler) WithGroup(name string) slog.Handler {
	c := h.clone()
	c.group = name
	return c
}

func (h *handler) setTerminalLevel(l Level) {
	st := h.state
	st.mu.Lock()
	st.termLvl = l.toSlog()
	st.mu.Unlock()
}

func (h *handler) terminalLevel() Level {
	st := h.state
	st.mu.Lock()
	defer st.mu.Unlock()
	return slogToLevel(st.termLvl)
}

func (h *handler) attachFile(path string, f *os.File) {
	st := h.state
	st.writeMu.Lock()
	defer st.writeMu.Unlock()
	if prev, ok := st.file.(*os.File); ok && prev != nil && prev != f {
		_ = prev.Close()
	}
	st.filePath = path
	st.file = f
}

func (st *handlerState) ensureLogFileReady() error {
	if st.filePath == "" {
		return nil
	}
	info, err := os.Stat(st.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return st.reopenLogFile()
		}
		return err
	}
	if info.Size() < maxLogFileBytes {
		return nil
	}
	return st.rotateLogFile()
}

func (st *handlerState) rotateLogFile() error {
	if f, ok := st.file.(*os.File); ok && f != nil {
		_ = f.Close()
		st.file = nil
	}
	if err := rotateIfNeeded(st.filePath); err != nil {
		return err
	}
	return st.reopenLogFile()
}

func (st *handlerState) reopenLogFile() error {
	f, err := openLogFile(st.filePath)
	if err != nil {
		return err
	}
	st.file = f
	return nil
}

func fileTS() string { return time.Now().Format("2006-01-02 15:04:05") }

func termTS() string { return time.Now().Format("15:04:05") }

func writeTerminal(out, errW io.Writer, lvl Level, msg string, noColor bool) error {
	label, color, reset := termStyle(lvl, noColor)
	line := fmt.Sprintf("%s %s %s%s%s  %s\n", termPrefix(noColor), termTS(), color, label, reset, msg)
	if lvl >= LevelWarn {
		_, err := fmt.Fprint(errW, line)
		return err
	}
	_, err := fmt.Fprint(out, line)
	return err
}

func termPrefix(noColor bool) string {
	if noColor {
		return "[aivm]"
	}
	return "\033[1m\033[34m[aivm]\033[0m"
}

func termStyle(lvl Level, noColor bool) (label, color, reset string) {
	if noColor {
		return lvl.String(), "", ""
	}
	reset = "\033[0m"
	switch lvl {
	case LevelTrace, LevelDebug:
		return lvl.String(), "\033[36m", reset
	case LevelInfo:
		return "INFO", "\033[32m", reset
	case LevelWarn:
		return "WARN", "\033[33m", reset
	default:
		return "ERROR", "\033[31m", reset
	}
}
