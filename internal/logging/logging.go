// Package logging provides a small wrapper around the standard library slog
// package, configured for structured JSON output suitable for log aggregation
// (ELK, Loki, CloudWatch) in a microservice environment.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a structured JSON logger at the requested level.
func New(level string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
