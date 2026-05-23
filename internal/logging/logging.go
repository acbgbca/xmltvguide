// Package logging provides a thin wrapper around log/slog for leveled logging.
//
// Call Init once at startup to configure the default slog handler. Other
// packages then use Debug/Info/Warn/Error which forward to slog. The package
// is designed to be a forward-looking replacement for log.Printf; existing
// log.Printf call sites can be migrated incrementally.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Init configures the default slog handler at the requested level, writing
// to os.Stderr. An unrecognised level falls back to info and emits a warning
// describing the bad value.
func Init(level string) {
	InitWithWriter(level, os.Stderr)
}

// InitWithWriter behaves like Init but routes output to w instead of os.Stderr.
// Intended for tests that need to capture output.
func InitWithWriter(level string, w io.Writer) {
	parsed, ok := parseLevel(level)
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: parsed})
	slog.SetDefault(slog.New(handler))
	if !ok {
		Warn(fmt.Sprintf("invalid log level %q; falling back to info", level))
	}
}

// parseLevel maps a case-insensitive level string to a slog.Level. The second
// return value is false when the input was not recognised; the caller is
// responsible for falling back and warning.
func parseLevel(level string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, true
	case "", "info":
		return slog.LevelInfo, level == "" || strings.EqualFold(level, "info")
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

// Debug emits a DEBUG-level log line.
func Debug(msg string, args ...any) {
	slog.Default().Log(context.Background(), slog.LevelDebug, msg, args...)
}

// Info emits an INFO-level log line.
func Info(msg string, args ...any) {
	slog.Default().Log(context.Background(), slog.LevelInfo, msg, args...)
}

// Warn emits a WARN-level log line.
func Warn(msg string, args ...any) {
	slog.Default().Log(context.Background(), slog.LevelWarn, msg, args...)
}

// Error emits an ERROR-level log line.
func Error(msg string, args ...any) {
	slog.Default().Log(context.Background(), slog.LevelError, msg, args...)
}
