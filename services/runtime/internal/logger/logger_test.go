package logger

import (
	"log/slog"
	"testing"
)

func TestNewReturnsLogger(t *testing.T) {
	cases := []struct {
		level  string
		format string
	}{
		{"debug", "json"},
		{"info", "json"},
		{"warn", "text"},
		{"error", "text"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		log := New(c.level, c.format)
		if log == nil {
			t.Errorf("New(%q, %q) returned nil", c.level, c.format)
		}
		if _, ok := any(log).(*slog.Logger); !ok {
			t.Errorf("expected *slog.Logger, got %T", log)
		}
	}
}
