// Package logger provides structured logging via slog.
//
// JSON format in production, text format in dev. Level from config.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New constructs a slog.Logger based on level (debug|info|warn|error) and
// format (json|text). Always writes to stderr.
func New(level, format string) *slog.Logger {
	return NewWithRing(level, format, nil)
}

// NewWithRing constructs a slog.Logger and additionally tees every
// record into the given Ring so the dashboard's /operate/logs tab can
// render recent lines without a separate log aggregator. Pass nil to
// get the same behaviour as New.
func NewWithRing(level, format string, ring *Ring) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info", "":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	if ring != nil {
		handler = NewRingHandler(handler, ring)
	}
	return slog.New(handler)
}
