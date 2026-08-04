package loom

import (
	"log/slog"
	"os"
)

// NewLogger returns a structured logger appropriate for env: a
// human-readable text handler when env is "dev", and a JSON handler for
// anything else (e.g. "prod", "staging").
func NewLogger(env string) *slog.Logger {
	var handler slog.Handler
	if env == "dev" {
		handler = slog.NewTextHandler(os.Stderr, nil)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	}
	return slog.New(handler)
}
