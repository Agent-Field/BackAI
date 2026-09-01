// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

func ptrTime(t time.Time) *time.Time { return &t }

// evaluateAttempt is the pure heart of the executor: it decides an outcome
// from a polled attempt. Table-drive every branch.
func TestEvaluateAttempt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	grace := 10 * time.Second

	cases := []struct {
		name string
		att  *RemoteAttempt
		want remoteOutcome
	}{
		{"nil keeps waiting", nil, outcomeContinue},
		{"completed", &RemoteAttempt{State: AttemptCompleted}, outcomeCompleted},
		{"failed retryable", &RemoteAttempt{State: AttemptFailed, Retryable: true}, outcomeFailedRetryable},
		{"failed permanent", &RemoteAttempt{State: AttemptFailed, Retryable: false}, outcomeFailedPermanent},
		{"superseded is permanent", &RemoteAttempt{State: AttemptSuperseded}, outcomeFailedPermanent},
		{"leased and fresh", &RemoteAttempt{State: AttemptLeased, LeaseExpiresAt: ptrTime(now.Add(5 * time.Second))}, outcomeContinue},
		{"leased and expired", &RemoteAttempt{State: AttemptLeased, LeaseExpiresAt: ptrTime(now.Add(-time.Second))}, outcomeLeaseExpired},
		{"ready and fresh", &RemoteAttempt{State: AttemptReady, CreatedAt: now.Add(-2 * time.Second)}, outcomeContinue},
		{"ready past grace", &RemoteAttempt{State: AttemptReady, CreatedAt: now.Add(-30 * time.Second)}, outcomeUnleased},
		{"canceled wins", &RemoteAttempt{State: AttemptLeased, Canceled: true, LeaseExpiresAt: ptrTime(now.Add(time.Minute))}, outcomeCanceled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := evaluateAttempt(c.att, now, grace); got != c.want {
				t.Fatalf("evaluateAttempt = %v, want %v", got, c.want)
			}
		})
	}
}

// remoteOutcomeToRiverErr must translate each outcome to the correct River
// return value: nil for success, a plain error for a retry, JobCancel for a
// permanent failure, JobSnooze for an unleased job.
func TestRemoteOutcomeToRiverErr(t *testing.T) {
	snooze := 7 * time.Second

	if err := remoteOutcomeToRiverErr(outcomeCompleted, nil, snooze, nil); err != nil {
		t.Fatalf("completed -> %v, want nil", err)
	}

	// Retryable failure: a plain error (River retries/backs off), NOT a
	// cancel or snooze.
	rerr := remoteOutcomeToRiverErr(outcomeFailedRetryable, &RemoteAttempt{Error: "boom"}, snooze, nil)
	if rerr == nil {
		t.Fatal("retryable -> nil, want error")
	}
	var jce *river.JobCancelError
	var jse *river.JobSnoozeError
	if errors.As(rerr, &jce) || errors.As(rerr, &jse) {
		t.Fatalf("retryable produced a cancel/snooze error: %v", rerr)
	}
	if rerr.Error() != "boom" {
		t.Fatalf("retryable error message = %q, want the worker's message", rerr.Error())
	}

	// Permanent failure: JobCancel (terminal, no retry).
	perr := remoteOutcomeToRiverErr(outcomeFailedPermanent, &RemoteAttempt{Error: "nope"}, snooze, nil)
	if !errors.As(perr, &jce) {
		t.Fatalf("permanent -> %v, want JobCancelError", perr)
	}

	// Lease expired: a plain retryable error.
	lerr := remoteOutcomeToRiverErr(outcomeLeaseExpired, nil, snooze, nil)
	if lerr == nil || errors.As(lerr, &jce) || errors.As(lerr, &jse) {
		t.Fatalf("lease-expired -> %v, want plain error", lerr)
	}

	// Unleased: JobSnooze with the configured duration (frees the slot).
	uerr := remoteOutcomeToRiverErr(outcomeUnleased, nil, snooze, nil)
	if !errors.As(uerr, &jse) {
		t.Fatalf("unleased -> %v, want JobSnoozeError", uerr)
	}
	if jse.Duration != snooze {
		t.Fatalf("snooze duration = %v, want %v", jse.Duration, snooze)
	}

	// Canceled surfaces the ctx error verbatim.
	ce := context.Canceled
	if got := remoteOutcomeToRiverErr(outcomeCanceled, nil, snooze, ce); !errors.Is(got, ce) {
		t.Fatalf("canceled -> %v, want ctx error", got)
	}
}

// awaitOutcome should return `completed` once a worker leases and completes
// the attempt.
func TestAwaitOutcomeCompleted(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 1, 1, tenantA, "k1")

	type res struct {
		o   remoteOutcome
		err error
	}
	ch := make(chan res, 1)
	go func() {
		o, _, err := awaitOutcome(ctx, s, tenantA, 1, 1, remoteParams{PollInterval: 2 * time.Millisecond, UnleasedGrace: time.Hour})
		ch <- res{o, err}
	}()

	// Simulate a worker.
	if _, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, tenantA, 1, 1, "w1", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	select {
	case r := <-ch:
		if r.o != outcomeCompleted || r.err != nil {
			t.Fatalf("awaitOutcome = (%v, %v), want (completed, nil)", r.o, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("awaitOutcome did not return")
	}
}

// awaitOutcome should detect a dead worker (lease TTL elapsed with no
// heartbeat) as outcomeLeaseExpired so River retries.
func TestAwaitOutcomeLeaseExpiry(t *testing.T) {
	ctx := context.Background()
	clk := newTestClock()
	s := newMemRemoteStore(clk.now)
	mustEnqueue(t, s, 1, 1, tenantA, "k1")
	if _, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	// Worker dies: advance past the TTL.
	clk.advance(31 * time.Second)

	type res struct {
		o   remoteOutcome
		err error
	}
	ch := make(chan res, 1)
	go func() {
		o, _, err := awaitOutcome(ctx, s, tenantA, 1, 1, remoteParams{PollInterval: 2 * time.Millisecond, UnleasedGrace: time.Hour, now: clk.now})
		ch <- res{o, err}
	}()
	select {
	case r := <-ch:
		if r.o != outcomeLeaseExpired {
			t.Fatalf("awaitOutcome = %v, want outcomeLeaseExpired", r.o)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("awaitOutcome did not return")
	}
}

// awaitOutcome should return `unleased` when no worker picks the job up
// within the grace window, so the executor can snooze and free the slot.
func TestAwaitOutcomeUnleased(t *testing.T) {
	ctx := context.Background()
	clk := newTestClock()
	s := newMemRemoteStore(clk.now)
	mustEnqueue(t, s, 1, 1, tenantA, "k1")
	clk.advance(20 * time.Second) // past a 10s grace

	o, _, err := awaitOutcome(ctx, s, tenantA, 1, 1, remoteParams{PollInterval: 2 * time.Millisecond, UnleasedGrace: 10 * time.Second, now: clk.now})
	if err != nil || o != outcomeUnleased {
		t.Fatalf("awaitOutcome = (%v, %v), want (unleased, nil)", o, err)
	}
}

// awaitOutcome should return `canceled` (with the ctx error) when the River
// job's context is cancelled, and should flag the attempt canceled so the
// worker learns of it on its next heartbeat.
func TestAwaitOutcomeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 1, 1, tenantA, "k1")

	type res struct {
		o   remoteOutcome
		err error
	}
	ch := make(chan res, 1)
	go func() {
		o, _, err := awaitOutcome(ctx, s, tenantA, 1, 1, remoteParams{PollInterval: 2 * time.Millisecond, UnleasedGrace: time.Hour})
		ch <- res{o, err}
	}()
	cancel()
	select {
	case r := <-ch:
		if r.o != outcomeCanceled || !errors.Is(r.err, context.Canceled) {
			t.Fatalf("awaitOutcome = (%v, %v), want (canceled, context.Canceled)", r.o, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("awaitOutcome did not return on cancel")
	}
	att, _ := s.GetAttempt(context.Background(), tenantA, 1, 1)
	if !att.Canceled {
		t.Fatal("attempt not flagged canceled after ctx cancellation")
	}
}
