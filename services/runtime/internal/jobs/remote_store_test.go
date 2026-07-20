// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// testClock is a controllable clock so TTL-expiry and unleased-grace paths
// are testable without wall-clock waits.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func mustEnqueue(t *testing.T, s *memRemoteStore, jobID int64, attempt int, tenant, kind string) {
	t.Helper()
	if _, err := s.EnqueueAttempt(context.Background(), RemoteAttempt{
		TenantID: tenant, JobID: jobID, Attempt: attempt, Kind: kind,
		Payload: json.RawMessage(`{"n":1}`), State: AttemptReady,
	}); err != nil {
		t.Fatalf("enqueue (%d,%d): %v", jobID, attempt, err)
	}
}

// Contract: EnqueueAttempt creates a `ready` attempt, is idempotent per
// (job, attempt), and supersedes older non-terminal attempts of the job.
func TestEnqueueAttemptIdempotentAndSupersedes(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)

	got, err := s.EnqueueAttempt(ctx, RemoteAttempt{TenantID: tenantA, JobID: 1, Attempt: 1, Kind: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != AttemptReady {
		t.Fatalf("state = %q, want ready", got.State)
	}

	// Idempotent: re-registering the same (job, attempt) returns ready and
	// doesn't error.
	again, err := s.EnqueueAttempt(ctx, RemoteAttempt{TenantID: tenantA, JobID: 1, Attempt: 1, Kind: "k1"})
	if err != nil || again.State != AttemptReady {
		t.Fatalf("re-enqueue: state=%q err=%v", again.State, err)
	}

	// A newer attempt supersedes the older one.
	if _, err := s.EnqueueAttempt(ctx, RemoteAttempt{TenantID: tenantA, JobID: 1, Attempt: 2, Kind: "k1"}); err != nil {
		t.Fatal(err)
	}
	old, err := s.GetAttempt(ctx, tenantA, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if old.State != AttemptSuperseded {
		t.Fatalf("old attempt state = %q, want superseded", old.State)
	}
	newer, _ := s.GetAttempt(ctx, tenantA, 1, 2)
	if newer.State != AttemptReady {
		t.Fatalf("new attempt state = %q, want ready", newer.State)
	}
}

// Contract: Lease returns the OLDEST ready attempt for the calling tenant
// among the kinds it declares, and NEVER another tenant's or another kind's
// attempt.
func TestLeaseTenantAndKindScoped(t *testing.T) {
	ctx := context.Background()
	clk := newTestClock()
	s := newMemRemoteStore(clk.now)

	mustEnqueue(t, s, 10, 1, tenantA, "k1")
	clk.advance(time.Second)
	mustEnqueue(t, s, 11, 1, tenantA, "k2")
	clk.advance(time.Second)
	mustEnqueue(t, s, 12, 1, tenantB, "k1")

	// tenantA asking for k1 gets its oldest k1 (job 10), not the k2 job and
	// not tenantB's k1 job.
	att, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if att == nil || att.JobID != 10 {
		t.Fatalf("lease = %+v, want job 10", att)
	}
	if att.State != AttemptLeased || att.WorkerID != "w1" {
		t.Fatalf("leased state=%q worker=%q", att.State, att.WorkerID)
	}

	// No more k1 ready for tenantA.
	none, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Fatalf("expected no lease, got job %d", none.JobID)
	}

	// tenantB's k1 is still leasable only by tenantB.
	bLease, err := s.Lease(ctx, LeaseRequest{TenantID: tenantB, Kinds: []string{"k1"}, WorkerID: "w2", TTL: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if bLease == nil || bLease.JobID != 12 {
		t.Fatalf("tenantB lease = %+v, want job 12", bLease)
	}
}

// Contract: a worker in tenant B can never lease tenant A's ready attempt.
func TestLeaseCrossTenantDenied(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 30, 1, tenantA, "k1")

	got, err := s.Lease(ctx, LeaseRequest{TenantID: tenantB, Kinds: []string{"k1"}, WorkerID: "w", TTL: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("tenantB leased tenantA's job %d — cross-tenant leak", got.JobID)
	}
}

// Contract: Heartbeat extends the lease, surfaces cancellation, and rejects a
// non-owning worker / missing attempt.
func TestHeartbeatAndCancel(t *testing.T) {
	ctx := context.Background()
	clk := newTestClock()
	s := newMemRemoteStore(clk.now)
	mustEnqueue(t, s, 10, 1, tenantA, "k1")
	leased, _ := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: 30 * time.Second})
	firstExp := *leased.LeaseExpiresAt

	clk.advance(10 * time.Second)
	hb, err := s.Heartbeat(ctx, tenantA, 10, 1, "w1", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if hb.Canceled {
		t.Fatal("unexpected canceled=true")
	}
	if !hb.LeaseExpiresAt.After(firstExp) {
		t.Fatalf("heartbeat did not extend lease: %v !> %v", hb.LeaseExpiresAt, firstExp)
	}

	// Wrong worker can't heartbeat.
	if _, err := s.Heartbeat(ctx, tenantA, 10, 1, "intruder", time.Second); !errors.Is(err, ErrAttemptNotLeased) {
		t.Fatalf("wrong worker heartbeat err = %v, want ErrAttemptNotLeased", err)
	}
	// Missing attempt.
	if _, err := s.Heartbeat(ctx, tenantA, 999, 1, "w1", time.Second); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("missing heartbeat err = %v, want ErrAttemptNotFound", err)
	}

	// Cancellation propagates to the next heartbeat. tenantB can't cancel
	// tenantA's job.
	if err := s.MarkCanceled(ctx, tenantB, 10); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("cross-tenant cancel err = %v, want ErrAttemptNotFound", err)
	}
	if err := s.MarkCanceled(ctx, tenantA, 10); err != nil {
		t.Fatal(err)
	}
	hb2, err := s.Heartbeat(ctx, tenantA, 10, 1, "w1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !hb2.Canceled {
		t.Fatal("heartbeat did not surface cancellation")
	}
}

// Contract: Complete transitions leased -> completed once; a repeat is a
// no-op conflict (ErrAttemptResolved); a non-owner gets ErrAttemptNotLeased.
func TestCompleteIdempotency(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 10, 1, tenantA, "k1")

	// Not yet leased -> completing gives not-leased.
	if err := s.Complete(ctx, tenantA, 10, 1, "w1", json.RawMessage(`{"ok":true}`)); !errors.Is(err, ErrAttemptNotLeased) {
		t.Fatalf("complete-before-lease err = %v, want ErrAttemptNotLeased", err)
	}

	if _, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	// Wrong worker.
	if err := s.Complete(ctx, tenantA, 10, 1, "other", json.RawMessage(`{}`)); !errors.Is(err, ErrAttemptNotLeased) {
		t.Fatalf("wrong-worker complete err = %v, want ErrAttemptNotLeased", err)
	}
	// Owner completes.
	if err := s.Complete(ctx, tenantA, 10, 1, "w1", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	done, _ := s.GetAttempt(ctx, tenantA, 10, 1)
	if done.State != AttemptCompleted || string(done.Result) != `{"ok":true}` {
		t.Fatalf("after complete: state=%q result=%s", done.State, done.Result)
	}
	// Re-complete is a no-op conflict.
	if err := s.Complete(ctx, tenantA, 10, 1, "w1", json.RawMessage(`{}`)); !errors.Is(err, ErrAttemptResolved) {
		t.Fatalf("re-complete err = %v, want ErrAttemptResolved", err)
	}
}

// Contract: Fail records the retryable flag and is likewise idempotent.
func TestFailRecordsRetryable(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 10, 1, tenantA, "k1")
	if _, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, tenantA, 10, 1, "w1", "boom", true); err != nil {
		t.Fatal(err)
	}
	att, _ := s.GetAttempt(ctx, tenantA, 10, 1)
	if att.State != AttemptFailed || att.Error != "boom" || !att.Retryable {
		t.Fatalf("after fail: state=%q error=%q retryable=%v", att.State, att.Error, att.Retryable)
	}
	if err := s.Fail(ctx, tenantA, 10, 1, "w1", "again", false); !errors.Is(err, ErrAttemptResolved) {
		t.Fatalf("re-fail err = %v, want ErrAttemptResolved", err)
	}
}

// Contract: after the lease TTL passes without a heartbeat, the attempt reads
// back as leased-and-expired — the signal the executor turns into a retry.
func TestLeaseTTLExpiry(t *testing.T) {
	ctx := context.Background()
	clk := newTestClock()
	s := newMemRemoteStore(clk.now)
	mustEnqueue(t, s, 10, 1, tenantA, "k1")
	if _, err := s.Lease(ctx, LeaseRequest{TenantID: tenantA, Kinds: []string{"k1"}, WorkerID: "w1", TTL: 30 * time.Second}); err != nil {
		t.Fatal(err)
	}
	clk.advance(31 * time.Second)
	att, _ := s.GetAttempt(ctx, tenantA, 10, 1)
	if att.State != AttemptLeased {
		t.Fatalf("state = %q, want leased", att.State)
	}
	if o := evaluateAttempt(att, clk.now(), 0); o != outcomeLeaseExpired {
		t.Fatalf("evaluateAttempt = %v, want outcomeLeaseExpired", o)
	}
}

// Contract: AppendLogs is tenant-scoped and requires the attempt to exist.
func TestAppendLogsTenantScoped(t *testing.T) {
	ctx := context.Background()
	s := newMemRemoteStore(newTestClock().now)
	mustEnqueue(t, s, 40, 1, tenantA, "k1")

	n, err := s.AppendLogs(ctx, tenantA, 40, 1, []LogLine{{Level: "info", Message: "hi"}})
	if err != nil || n != 1 {
		t.Fatalf("append logs n=%d err=%v", n, err)
	}
	if _, err := s.AppendLogs(ctx, tenantB, 40, 1, []LogLine{{Message: "x"}}); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("cross-tenant logs err = %v, want ErrAttemptNotFound", err)
	}
	if _, err := s.AppendLogs(ctx, tenantA, 999, 1, []LogLine{{Message: "x"}}); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("missing-attempt logs err = %v, want ErrAttemptNotFound", err)
	}
}
