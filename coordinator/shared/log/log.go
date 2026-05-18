// Package log centralises slog configuration for iogrid coordinator services.
// All services emit JSON-formatted logs to stdout so they can be ingested
// uniformly by Loki / Fluent Bit downstream.
package log

import (
	"log/slog"
	"os"
	"strings"
)

// Setup configures the global slog default logger with a JSON handler.
// The level is read from the LOG_LEVEL env var (debug|info|warn|error),
// defaulting to info. The "service" attribute is set on every log line so
// log queries can filter by microservice without parsing the source path.
func Setup(serviceName string) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
	})
	logger := slog.New(handler).With(
		slog.String("service", serviceName),
	)
	slog.SetDefault(logger)
	return logger
}
