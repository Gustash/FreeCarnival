// Package logger provides colored console logging for FreeCarnival.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorGray   = "\033[90m"
)

var (
	defaultLogger *slog.Logger
	currentLevel  slog.Level = slog.LevelInfo
	currentOutput io.Writer  = os.Stderr
)

// Level represents the logging level.
type Level int

const (
	// LevelDebug includes all messages.
	LevelDebug Level = iota
	// LevelInfo includes info, warn, and error messages (default).
	LevelInfo
	// LevelWarn includes warn and error messages.
	LevelWarn
	// LevelError includes only error messages.
	LevelError
)

// ConsoleHandler is a custom slog.Handler that formats logs with colors.
type ConsoleHandler struct {
	output io.Writer
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	// Get the caller filename
	var filename string
	if r.PC != 0 {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		filename = filepath.Base(f.File)
	}

	// Determine level color and label
	var levelColor, levelLabel string
	switch r.Level {
	case slog.LevelDebug:
		levelColor = colorGray
		levelLabel = "DEBUG"
	case slog.LevelInfo:
		levelColor = colorBlue
		levelLabel = "INFO"
	case slog.LevelWarn:
		levelColor = colorYellow
		levelLabel = "WARN"
	case slog.LevelError:
		levelColor = colorRed
		levelLabel = "ERROR"
	default:
		levelColor = colorReset
		levelLabel = "UNKNOWN"
	}

	// Build the message with attributes
	var sb strings.Builder
	sb.WriteString(r.Message)

	// Helper to format attribute
	formatAttr := func(a slog.Attr, first *bool) {
		if !*first {
			sb.WriteString(" ")
		}
		*first = false
		sb.WriteString(fmt.Sprintf("%s=%v", a.Key, a.Value.Any()))
	}

	// Add stored attributes from WithAttrs
	if len(h.attrs) > 0 {
		sb.WriteString(" ")
		first := true
		for _, a := range h.attrs {
			formatAttr(a, &first)
		}
	}

	// Add record attributes
	if r.NumAttrs() > 0 {
		if len(h.attrs) == 0 {
			sb.WriteString(" ")
		}
		first := len(h.attrs) == 0
		r.Attrs(func(a slog.Attr) bool {
			formatAttr(a, &first)
			return true
		})
	}

	// Format: [LEVEL] filename: message
	if filename != "" {
		fmt.Fprintf(h.output, "%s[%s]%s %s: %s\n",
			levelColor, levelLabel, colorReset, filename, sb.String())
	} else {
		fmt.Fprintf(h.output, "%s[%s]%s %s\n",
			levelColor, levelLabel, colorReset, sb.String())
	}

	return nil
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &ConsoleHandler{
		output: h.output,
		level:  h.level,
		attrs:  newAttrs,
		groups: h.groups,
	}
}

func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	// For simplicity, return the same handler
	// In a full implementation, you'd track groups
	return h
}

func init() {
	// Default to Info level with colored console output
	rebuildLogger()
}

// rebuildLogger recreates the logger with current settings.
func rebuildLogger() {
	handler := &ConsoleHandler{
		output: currentOutput,
		level:  currentLevel,
	}
	defaultLogger = slog.New(handler)
}

// SetLevel configures the global logger with the specified level.
func SetLevel(level Level) {
	switch level {
	case LevelDebug:
		currentLevel = slog.LevelDebug
	case LevelInfo:
		currentLevel = slog.LevelInfo
	case LevelWarn:
		currentLevel = slog.LevelWarn
	case LevelError:
		currentLevel = slog.LevelError
	default:
		currentLevel = slog.LevelInfo
	}
	rebuildLogger()
}

// SetOutput changes the output destination for logs.
func SetOutput(w io.Writer) {
	currentOutput = w
	rebuildLogger()
}

// log is a helper that logs with proper caller information.
func log(level slog.Level, msg string, args ...any) {
	if !defaultLogger.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip [Callers, log, Debug/Info/Warn/Error]
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = defaultLogger.Handler().Handle(context.Background(), r)
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	log(slog.LevelDebug, msg, args...)
}

// Info logs an informational message.
func Info(msg string, args ...any) {
	log(slog.LevelInfo, msg, args...)
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	log(slog.LevelWarn, msg, args...)
}

// Error logs an error message.
func Error(msg string, args ...any) {
	log(slog.LevelError, msg, args...)
}

// Printf provides fmt.Printf-style logging for backward compatibility.
// Uses Info level.
func Printf(format string, args ...any) {
	log(slog.LevelInfo, format, args...)
}

// Println provides fmt.Println-style logging for backward compatibility.
// Uses Info level.
func Println(msg string) {
	log(slog.LevelInfo, msg)
}

// With returns a new logger with additional attributes.
func With(args ...any) *slog.Logger {
	return defaultLogger.With(args...)
}

// GetLogger returns the underlying slog.Logger for advanced usage.
func GetLogger() *slog.Logger {
	return defaultLogger
}

