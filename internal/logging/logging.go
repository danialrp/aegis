// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logging builds the controller's slog handler.
//
// JSON output is the default (production); text is available for
// human-readable local dev. Level parsing accepts the four standard
// names and falls back to info on anything unknown.
package logging

import (
	"io"
	"log/slog"
	"strings"
)

// New constructs a slog.Logger writing to w with the given level and
// format ("json" or "text"). Unknown levels default to info; unknown
// formats default to json.
func New(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var h slog.Handler
	switch strings.ToLower(format) {
	case "text":
		h = slog.NewTextHandler(w, opts)
	default:
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h)
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
