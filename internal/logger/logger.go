// Package logger provides a simple structured logger compatible with Go 1.19.
// It replaces log/slog (which requires Go 1.21+) with an equivalent API.
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents the severity of a log message.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger is a simple structured logger that writes key-value pairs to an
// io.Writer. It is safe for concurrent use.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	level  Level
	prefix string
}

// New creates a Logger that writes to the given writer at the given level.
func New(out io.Writer, level Level) *Logger {
	return &Logger{out: out, level: level}
}

func (l *Logger) log(level Level, msg string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Format("2006/01/02 15:04:05")

	// Format: "2006/01/02 15:04:05 LEVEL msg key1=val1 key2=val2"
	fmt.Fprintf(l.out, "%s %s %s", now, level, msg)
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			fmt.Fprintf(l.out, " %s=%v", args[i], args[i+1])
		} else {
			fmt.Fprintf(l.out, " %v", args[i])
		}
	}
	fmt.Fprintln(l.out)
}

// Debug logs a message at DEBUG level.
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.log(LevelDebug, msg, args...)
}

// Info logs a message at INFO level.
func (l *Logger) Info(msg string, args ...interface{}) {
	l.log(LevelInfo, msg, args...)
}

// Warn logs a message at WARN level.
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.log(LevelWarn, msg, args...)
}

// Error logs a message at ERROR level.
func (l *Logger) Error(msg string, args ...interface{}) {
	l.log(LevelError, msg, args...)
}

// ── Default package-level logger ──────────────────────────────────────────────

var defaultLogger = New(os.Stderr, LevelInfo)

// Default returns the package-level Logger.
func Default() *Logger {
	return defaultLogger
}

// SetDefault sets the package-level Logger.
func SetDefault(l *Logger) {
	defaultLogger = l
}

// Debug logs at DEBUG level via the default logger.
func Debug(msg string, args ...interface{}) {
	defaultLogger.log(LevelDebug, msg, args...)
}

// Info logs at INFO level via the default logger.
func Info(msg string, args ...interface{}) {
	defaultLogger.log(LevelInfo, msg, args...)
}

// Warn logs at WARN level via the default logger.
func Warn(msg string, args ...interface{}) {
	defaultLogger.log(LevelWarn, msg, args...)
}

// Error logs at ERROR level via the default logger.
func Error(msg string, args ...interface{}) {
	defaultLogger.log(LevelError, msg, args...)
}
