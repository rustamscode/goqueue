// Package log provides structured logging utilities for GoQueue.
package log

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// LoggerKey is the context key for storing a logger.
	LoggerKey contextKey = "logger"
)

// Config holds logging configuration options.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// Format is the output format (json or text).
	Format string
	// Output is the writer for log output. Defaults to os.Stderr.
	Output io.Writer
	// AddSource adds source file information to log entries.
	AddSource bool
}

// DefaultConfig returns the default logging configuration.
func DefaultConfig() Config {
	return Config{
		Level:     "info",
		Format:    "json",
		Output:    os.Stderr,
		AddSource: false,
	}
}

// NewLogger creates a new structured logger with the given configuration.
func NewLogger(cfg Config) *slog.Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stderr
	}

	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(cfg.Output, opts)
	} else {
		handler = slog.NewJSONHandler(cfg.Output, opts)
	}

	return slog.New(handler)
}

// parseLevel converts a string level to slog.Level.
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithLogger adds a logger to the context.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, logger)
}

// FromContext retrieves the logger from context, or returns the default logger.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(LoggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// With returns a new logger with additional attributes.
func With(logger *slog.Logger, attrs ...any) *slog.Logger {
	return logger.With(attrs...)
}

// Error logs an error with the given message and attributes.
func Error(ctx context.Context, msg string, attrs ...any) {
	FromContext(ctx).Error(msg, attrs...)
}

// Warn logs a warning with the given message and attributes.
func Warn(ctx context.Context, msg string, attrs ...any) {
	FromContext(ctx).Warn(msg, attrs...)
}

// Info logs an info message with the given attributes.
func Info(ctx context.Context, msg string, attrs ...any) {
	FromContext(ctx).Info(msg, attrs...)
}

// Debug logs a debug message with the given attributes.
func Debug(ctx context.Context, msg string, attrs ...any) {
	FromContext(ctx).Debug(msg, attrs...)
}
