package logging

import (
	"log"
	"log/slog"
	"os"
)

// NewLogger creates a structured JSON logger for production use.
// In development, it falls back to a text handler for readability.
func NewLogger(env string) (*slog.Logger, *log.Logger) {
	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	structured := slog.New(handler)
	// Bridge: create a standard log.Logger that writes through slog
	bridge := slog.NewLogLogger(handler, slog.LevelInfo)
	return structured, bridge
}
