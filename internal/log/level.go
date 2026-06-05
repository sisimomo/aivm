package log

import (
	"fmt"
	"log/slog"
	"strings"
)

// Level controls which messages are emitted. More verbose levels include quieter ones.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLevel parses a log level name (debug, info, warn, error).
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level %q (use debug, info, warn, or error)", s)
	}
}

func (l Level) allows(msg Level) bool {
	return l <= msg
}

func (l Level) verbose() bool {
	return l == LevelDebug
}

// ToolMode is true at log level error — suppress non-failure noise from aivm and subprocesses.
func ToolMode() bool {
	return Default.level == LevelError
}

// SetLevel configures the default logger level and syncs slog.
func SetLevel(l Level) {
	Default.level = l
	var slogLevel slog.Level
	switch l {
	case LevelDebug:
		slogLevel = slog.LevelDebug
	case LevelInfo:
		slogLevel = slog.LevelInfo
	case LevelWarn:
		slogLevel = slog.LevelWarn
	case LevelError:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(Default.Err, &slog.HandlerOptions{Level: slogLevel})))
}
