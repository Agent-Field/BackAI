// SPDX-License-Identifier: Apache-2.0

// Package logger provides structured logging via slog.
//
// JSON format in production, text format in dev. Level from config.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

type HandlerWrapper func(slog.Handler) slog.Handler

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
	return NewWithRingAndWrapper(level, format, ring, nil)
}

// NewWithRingAndWrapper constructs a logger and lets callers wrap the final
// handler. It is used for optional write-side integrations such as Sentry
// while preserving the existing ring/stderr behavior.
func NewWithRingAndWrapper(level, format string, ring *Ring, wrap HandlerWrapper) *slog.Logger {
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
	if wrap != nil {
		handler = wrap(handler)
	}
	return slog.New(handler)
}

type teeHandler struct {
	primary slog.Handler
	extra   slog.Handler
}

func NewTeeHandler(primary, extra slog.Handler) slog.Handler {
	if extra == nil {
		return primary
	}
	return &teeHandler{primary: primary, extra: extra}
}

func (h *teeHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.primary.Enabled(ctx, lvl) || h.extra.Enabled(ctx, lvl)
}

func (h *teeHandler) Handle(ctx context.Context, rec slog.Record) error {
	err := h.primary.Handle(ctx, rec)
	if h.extra.Enabled(ctx, rec.Level) {
		if extraErr := h.extra.Handle(ctx, rec); err == nil {
			err = extraErr
		}
	}
	return err
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{primary: h.primary.WithAttrs(attrs), extra: h.extra.WithAttrs(attrs)}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{primary: h.primary.WithGroup(name), extra: h.extra.WithGroup(name)}
}
