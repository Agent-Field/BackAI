// SPDX-License-Identifier: Apache-2.0

package crons

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

func TestSystemSchedulerRunNow(t *testing.T) {
	s := NewSystemScheduler(slog.Default())
	called := 0
	if err := s.RegisterSystem("retention.daily", "@daily", func(context.Context) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("RegisterSystem: %v", err)
	}
	if err := s.RunNow(context.Background(), "retention.daily"); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
}

func TestSystemSchedulerTickRunsDueJobs(t *testing.T) {
	s := NewSystemScheduler(slog.Default())
	called := 0
	if err := s.RegisterSystem("retention.daily", "@daily", func(context.Context) error {
		called++
		return nil
	}); err != nil {
		t.Fatalf("RegisterSystem: %v", err)
	}
	s.mu.Lock()
	s.jobs["retention.daily"].next = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if processed := s.Tick(context.Background(), time.Now()); processed != 1 {
		t.Fatalf("Tick processed = %d, want 1", processed)
	}
	if called != 1 {
		t.Fatalf("called = %d, want 1", called)
	}
}

func TestSystemSchedulerValidation(t *testing.T) {
	s := NewSystemScheduler(slog.Default())
	if err := s.RegisterSystem("", "@daily", func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty name err = %v, want ErrInvalidInput", err)
	}
	if err := s.RegisterSystem("bad", "not a cron", func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad schedule err = %v, want ErrInvalidInput", err)
	}
	if err := s.RegisterSystem("missing-handler", "@daily", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil handler err = %v, want ErrInvalidInput", err)
	}
}
