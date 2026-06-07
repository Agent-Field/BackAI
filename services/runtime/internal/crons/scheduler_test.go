// SPDX-License-Identifier: Apache-2.0

// scheduler_test.go — unit tests for the parsing helpers + the
// nil-safety properties of the Scheduler.
//
// Full end-to-end coverage (DB + jobs queue) is exercised by the
// integration tests next door; this file isolates the pure logic so
// CI can run it without Postgres.
package crons

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseScheduleValid(t *testing.T) {
	cases := []string{
		"@hourly",
		"@daily",
		"@weekly",
		"@monthly",
		"@yearly",
		"@annually",
		"0 * * * *",
		"*/5 * * * *",
		"15 14 1 * *",
		"0 22 * * 1-5",
		"5,35 * * * *",
	}
	for _, expr := range cases {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			sched, err := ParseSchedule(expr)
			if err != nil {
				t.Fatalf("ParseSchedule(%q) returned err: %v", expr, err)
			}
			if sched == nil {
				t.Fatalf("ParseSchedule(%q) returned nil schedule", expr)
			}
			next := sched.Next(time.Now())
			if next.IsZero() {
				t.Fatalf("ParseSchedule(%q).Next returned zero time", expr)
			}
		})
	}
}

func TestParseScheduleInvalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"not a cron",
		"61 * * * *",
		"* * * * * *", // 6 fields = seconds dialect, which we disable
		"garbage extra",
	}
	for _, expr := range cases {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			_, err := ParseSchedule(expr)
			if err == nil {
				t.Fatalf("ParseSchedule(%q) did not error", expr)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ParseSchedule(%q) err = %v, want ErrInvalidInput", expr, err)
			}
		})
	}
}

func TestNextRunAdvances(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	next, err := NextRun("@hourly", now)
	if err != nil {
		t.Fatal(err)
	}
	if !next.After(now) {
		t.Fatalf("NextRun(@hourly) = %v, want after %v", next, now)
	}
	want := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("NextRun(@hourly) = %v, want %v", next, want)
	}
}

func TestNextRunInvalidExpression(t *testing.T) {
	if _, err := NextRun("garbage", time.Now()); err == nil {
		t.Fatal("NextRun(garbage) did not error")
	}
}

// stubEnqueuer satisfies JobEnqueuer for nil-safety tests.
type stubEnqueuer struct {
	count int
	last  string
}

func (s *stubEnqueuer) Enqueue(ctx context.Context, name string, args json.RawMessage, tenantID string) error {
	s.count++
	s.last = name
	return nil
}

func TestNewSchedulerNilDeps(t *testing.T) {
	if s := NewScheduler(SchedulerConfig{}); s != nil {
		t.Fatal("NewScheduler with empty config returned non-nil")
	}
	if s := NewScheduler(SchedulerConfig{Store: &Store{}}); s != nil {
		t.Fatal("NewScheduler missing enqueuer returned non-nil")
	}
	if s := NewScheduler(SchedulerConfig{Enqueuer: &stubEnqueuer{}}); s != nil {
		t.Fatal("NewScheduler missing store returned non-nil")
	}
}

func TestSchedulerTickNoPool(t *testing.T) {
	// Store with nil pool returns ErrNotConfigured from ClaimDue. The
	// scheduler should log + return 0 rather than crash.
	enq := &stubEnqueuer{}
	s := NewScheduler(SchedulerConfig{
		Store:    NewStore(nil, nil),
		Enqueuer: enq,
		Interval: 10 * time.Millisecond,
	})
	if s == nil {
		t.Fatal("NewScheduler returned nil for valid input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	processed := s.Tick(ctx)
	if processed != 0 {
		t.Fatalf("Tick processed = %d, want 0", processed)
	}
	if enq.count != 0 {
		t.Fatalf("enqueuer called %d times, want 0", enq.count)
	}
}

func TestSchedulerRunStopsOnContext(t *testing.T) {
	enq := &stubEnqueuer{}
	s := NewScheduler(SchedulerConfig{
		Store:    NewStore(nil, nil),
		Enqueuer: enq,
		Interval: 5 * time.Millisecond,
	})
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatal("Scheduler.Run did not exit within 1s of ctx cancel")
	}
}