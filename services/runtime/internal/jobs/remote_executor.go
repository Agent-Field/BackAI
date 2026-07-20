// SPDX-License-Identifier: Apache-2.0

// remote_executor.go bridges River's in-process execution to an
// out-of-process worker via the DB-backed rendezvous (RemoteStore).
//
// When the umbrella worker (worker.go) dispatches a remote-language job it
// calls Manager.runRemoteJob, which:
//
//  1. binds the job's tenant onto the context (so RLS + the pool's
//     PrepareConn hook scope every write to that tenant),
//  2. registers a `ready` attempt in the store, and
//  3. blocks — polling the attempt — until it reaches a terminal state,
//     the lease TTL expires, or the River job is cancelled.
//
// The blocking keeps the River job in `running`, so River retains full
// durability + retry semantics. The mapping from the observed rendezvous
// outcome back to a River Work() return value is:
//
//	completed          -> nil                (River: completed)
//	failed  retryable  -> error              (River: retry/backoff, then discard at max attempts)
//	failed  permanent  -> river.JobCancel    (River: cancelled, no retry — dead-letter)
//	lease expired      -> error              (River: retry -> a fresh attempt re-registers)
//	unleased too long  -> river.JobSnooze    (free the River slot; retry later, no attempt consumed)
//	cancelled (ctx)    -> ctx.Err()          (River: cancelled) + worker told to abort via heartbeat
package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// defaultTenantID mirrors tenancy.DefaultTenantID. Duplicated here to avoid
// an import edge from jobs -> tenancy; the value is a stable seed constant.
const defaultTenantID = "00000000-0000-0000-0000-000000000000"

// remoteOutcome is the terminal (or continue) signal derived from a polled
// attempt.
type remoteOutcome int

const (
	// outcomeContinue: the attempt is still in progress; keep polling.
	outcomeContinue remoteOutcome = iota
	outcomeCompleted
	outcomeFailedRetryable
	outcomeFailedPermanent
	outcomeLeaseExpired
	outcomeUnleased
	outcomeCanceled
)

// remoteParams tunes the executor's polling. Injected so tests can run the
// loop deterministically without wall-clock waits.
type remoteParams struct {
	// PollInterval is how often the executor re-reads the attempt.
	PollInterval time.Duration
	// UnleasedGrace is how long an attempt may sit `ready` (no worker) before
	// the executor snoozes to free the River worker slot.
	UnleasedGrace time.Duration
	// SnoozeDuration is how long River waits before re-running an unleased
	// job. Snoozing does not consume a retry attempt.
	SnoozeDuration time.Duration
	// now is the clock (defaults to time.Now).
	now func() time.Time
}

func (p remoteParams) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// defaultRemoteParams are the production defaults.
func defaultRemoteParams() remoteParams {
	return remoteParams{
		PollInterval:   time.Second,
		UnleasedGrace:  2 * time.Minute,
		SnoozeDuration: 10 * time.Second,
	}
}

// evaluateAttempt maps a polled attempt to an outcome. Pure — no I/O, no
// clock of its own — so the state machine is unit-testable in isolation.
func evaluateAttempt(att *RemoteAttempt, now time.Time, unleasedGrace time.Duration) remoteOutcome {
	if att == nil {
		return outcomeContinue
	}
	if att.Canceled {
		return outcomeCanceled
	}
	switch att.State {
	case AttemptCompleted:
		return outcomeCompleted
	case AttemptFailed:
		if att.Retryable {
			return outcomeFailedRetryable
		}
		return outcomeFailedPermanent
	case AttemptSuperseded:
		// A newer attempt replaced this one — stop this executor cleanly.
		return outcomeFailedPermanent
	case AttemptLeased:
		if att.LeaseExpiresAt != nil && now.After(*att.LeaseExpiresAt) {
			return outcomeLeaseExpired
		}
		return outcomeContinue
	case AttemptReady:
		if unleasedGrace > 0 && now.Sub(att.CreatedAt) > unleasedGrace {
			return outcomeUnleased
		}
		return outcomeContinue
	default:
		return outcomeContinue
	}
}

// awaitOutcome polls the store until the attempt reaches a non-continue
// outcome, the context is cancelled, or a non-transient store error occurs.
// It returns the outcome, the last-seen attempt row, and any error.
func awaitOutcome(ctx context.Context, store RemoteStore, tenantID string, jobID int64, attempt int, p remoteParams) (remoteOutcome, *RemoteAttempt, error) {
	interval := p.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// River is cancelling this job (or shutting down). Tell the
			// worker to abort via its next heartbeat, using a fresh
			// tenant-bound context (ctx is already cancelled).
			bg, cancel := context.WithTimeout(tenantctx.WithTenant(context.Background(), tenantID, ""), 5*time.Second)
			_ = store.MarkCanceled(bg, tenantID, jobID)
			cancel()
			return outcomeCanceled, nil, ctx.Err()
		case <-ticker.C:
			cur, err := store.GetAttempt(ctx, tenantID, jobID, attempt)
			if err != nil {
				if errors.Is(err, ErrAttemptNotFound) {
					// Row not visible yet (just-registered) — keep polling.
					continue
				}
				return outcomeContinue, nil, err
			}
			if o := evaluateAttempt(cur, p.clock(), p.UnleasedGrace); o != outcomeContinue {
				return o, cur, nil
			}
		}
	}
}

// remoteOutcomeToRiverErr maps a resolved outcome to the value Work() must
// return to River.
func remoteOutcomeToRiverErr(o remoteOutcome, att *RemoteAttempt, snooze time.Duration, ctxErr error) error {
	switch o {
	case outcomeCompleted:
		return nil
	case outcomeFailedRetryable:
		return errors.New(attemptError(att, "remote worker reported a retryable failure"))
	case outcomeFailedPermanent:
		return river.JobCancel(errors.New(attemptError(att, "remote worker reported a permanent failure")))
	case outcomeLeaseExpired:
		return errors.New("remote worker lease expired (worker died or lost connectivity)")
	case outcomeUnleased:
		if snooze <= 0 {
			snooze = 10 * time.Second
		}
		return river.JobSnooze(snooze)
	case outcomeCanceled:
		if ctxErr != nil {
			return ctxErr
		}
		return river.JobCancel(errors.New("remote job canceled"))
	default:
		if ctxErr != nil {
			return ctxErr
		}
		return errors.New("remote job ended without a terminal outcome")
	}
}

func attemptError(att *RemoteAttempt, fallback string) string {
	if att != nil && att.Error != "" {
		return att.Error
	}
	return fallback
}

// runRemoteJob is the entry point worker.go calls for a remote-language
// job. It registers the rendezvous attempt and blocks on the outcome.
func (m *Manager) runRemoteJob(ctx context.Context, jobID int64, attempt int, kind, tenantID string, payload []byte, deadline *time.Time) error {
	if m.remote == nil {
		return errors.New("jobs: remote worker store not configured")
	}
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	// Bind the job's tenant so RLS + the pool's PrepareConn hook scope every
	// rendezvous write to that tenant (the executor runs on a background
	// River context that carries no tenant of its own).
	ctx = tenantctx.WithTenant(ctx, tenantID, "")

	att := RemoteAttempt{
		TenantID: tenantID,
		JobID:    jobID,
		Attempt:  attempt,
		Kind:     kind,
		Payload:  payload,
		State:    AttemptReady,
		Deadline: deadline,
	}
	if _, err := m.remote.EnqueueAttempt(ctx, att); err != nil {
		// Return a plain error so River retries — a transient DB blip
		// shouldn't drop the job.
		return fmt.Errorf("jobs: register remote attempt: %w", err)
	}

	p := m.remoteParams
	outcome, cur, err := awaitOutcome(ctx, m.remote, tenantID, jobID, attempt, p)
	return remoteOutcomeToRiverErr(outcome, cur, p.SnoozeDuration, err)
}
