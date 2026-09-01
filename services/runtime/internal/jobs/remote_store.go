// SPDX-License-Identifier: Apache-2.0

// remote_store.go defines the pull-based remote-worker lease protocol's
// storage contract (PRD R3).
//
// A remote (python/typescript) job has no in-process Go handler. Instead,
// when the River executor picks such a job up it registers a leasable
// "attempt" (an entry in the RemoteStore) and blocks on it. An
// out-of-process worker leases the attempt, runs the user handler, and
// reports back — completion, failure (retryable or permanent), or a
// heartbeat that extends its lease.
//
// The store is the single source of truth for the rendezvous. Two
// implementations exist:
//
//   - memRemoteStore   (remote_store_mem.go): an in-memory, mutex-guarded
//     implementation used by unit tests. It is a *faithful* model of the
//     state machine, so the contract tests that run against it validate
//     real behaviour, not a mock.
//   - pgRemoteStore    (remote_store_pg.go): the Postgres-backed
//     implementation used in production. Tenant isolation is enforced both
//     at the app layer (an explicit tenant_id predicate) and at the DB
//     (FORCE ROW LEVEL SECURITY on suite_job_leases).
//
// The state machine, expressed as observable behaviour:
//
//	EnqueueAttempt : creates a `ready` attempt; idempotent per (job, attempt);
//	                 supersedes older non-terminal attempts of the same job.
//	Lease          : atomically claims the oldest `ready` attempt for the
//	                 tenant whose kind the worker declared; -> `leased`.
//	Heartbeat      : extends a `leased` attempt's TTL; surfaces `canceled`.
//	Complete       : `leased` -> `completed` (stores result). Re-complete of
//	                 an already-resolved attempt is a no-op ErrAttemptResolved.
//	Fail           : `leased` -> `failed` (stores error + retryable flag).
//	MarkCanceled   : flips `canceled` on the active attempt so the next
//	                 heartbeat tells the worker to abort.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// AttemptState is the lifecycle state of a remote job attempt.
type AttemptState string

const (
	// AttemptReady is leasable — waiting for a worker to pick it up.
	AttemptReady AttemptState = "ready"
	// AttemptLeased is held by a worker; the lease expires at LeaseExpiresAt.
	AttemptLeased AttemptState = "leased"
	// AttemptCompleted is terminal-success; Result holds the worker output.
	AttemptCompleted AttemptState = "completed"
	// AttemptFailed is terminal-failure; Error + Retryable describe it.
	AttemptFailed AttemptState = "failed"
	// AttemptSuperseded means a newer attempt of the same job replaced this
	// one (e.g. a River retry). Terminal and non-leasable.
	AttemptSuperseded AttemptState = "superseded"
)

// Terminal reports whether the state is final (no further transitions).
func (s AttemptState) Terminal() bool {
	switch s {
	case AttemptCompleted, AttemptFailed, AttemptSuperseded:
		return true
	case AttemptReady, AttemptLeased:
		return false
	default:
		return false
	}
}

// RemoteAttempt is one row of the rendezvous — a single (job_id, attempt).
type RemoteAttempt struct {
	TenantID       string
	JobID          int64
	Attempt        int
	Kind           string
	Payload        json.RawMessage
	State          AttemptState
	WorkerID       string
	LeaseExpiresAt *time.Time
	Deadline       *time.Time
	Result         json.RawMessage
	Error          string
	Retryable      bool
	Canceled       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LogLine is a structured log entry a worker attaches to a run.
type LogLine struct {
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Fields  json.RawMessage `json:"fields,omitempty"`
	At      *time.Time      `json:"at,omitempty"`
}

// Sentinel errors returned by store implementations. Handlers map these to
// the structured HTTP envelope (404 / 409 / …).
var (
	// ErrAttemptNotFound: no attempt matches (job_id, attempt) for the
	// tenant — 404.
	ErrAttemptNotFound = errors.New("jobs: remote attempt not found")
	// ErrAttemptNotLeased: the attempt exists but isn't leased by the
	// calling worker (wrong worker id, or not in `leased`) — 409.
	ErrAttemptNotLeased = errors.New("jobs: remote attempt not leased by worker")
	// ErrAttemptResolved: the attempt is already terminal; the operation is
	// a no-op — 409 (idempotent re-report).
	ErrAttemptResolved = errors.New("jobs: remote attempt already resolved")
)

// LeaseRequest parameters a worker sends when claiming an attempt.
type LeaseRequest struct {
	TenantID string
	Kinds    []string
	WorkerID string
	TTL      time.Duration
}

// RemoteStore is the storage contract for the lease protocol. All methods
// are tenant-scoped: a worker key operating in tenant T can never observe
// or mutate another tenant's attempts.
type RemoteStore interface {
	// EnqueueAttempt registers a `ready` attempt for the executor. It is
	// idempotent for a given (JobID, Attempt): re-registering returns the
	// existing row unchanged. Older non-terminal attempts of the same job
	// are marked `superseded`.
	EnqueueAttempt(ctx context.Context, att RemoteAttempt) (*RemoteAttempt, error)

	// Lease atomically claims the oldest `ready` attempt for req.TenantID
	// whose kind is in req.Kinds, marking it `leased` with a fresh TTL.
	// Returns (nil, nil) when nothing is available.
	Lease(ctx context.Context, req LeaseRequest) (*RemoteAttempt, error)

	// Heartbeat extends the lease TTL on an attempt held by workerID and
	// returns the current row (so the caller sees Canceled). Returns
	// ErrAttemptNotFound / ErrAttemptNotLeased on mismatch.
	Heartbeat(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, ttl time.Duration) (*RemoteAttempt, error)

	// Complete transitions a `leased` attempt (held by workerID) to
	// `completed`, storing result. ErrAttemptResolved if already terminal.
	Complete(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, result json.RawMessage) error

	// Fail transitions a `leased` attempt (held by workerID) to `failed`,
	// storing errMsg + retryable. ErrAttemptResolved if already terminal.
	Fail(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, errMsg string, retryable bool) error

	// GetAttempt returns the current row for (jobID, attempt). The executor
	// polls this. tenantID scopes the read.
	GetAttempt(ctx context.Context, tenantID string, jobID int64, attempt int) (*RemoteAttempt, error)

	// MarkCanceled flips Canceled=true on the active (non-terminal) attempt
	// of the job so the next heartbeat instructs the worker to abort.
	MarkCanceled(ctx context.Context, tenantID string, jobID int64) error

	// AppendLogs attaches structured log lines to a run.
	AppendLogs(ctx context.Context, tenantID string, jobID int64, attempt int, lines []LogLine) (int, error)
}

// cloneAttempt returns a deep-ish copy so callers can't mutate store state
// through the returned pointer. Payload/Result are treated as immutable
// (callers never mutate the underlying bytes).
func cloneAttempt(a *RemoteAttempt) *RemoteAttempt {
	if a == nil {
		return nil
	}
	out := *a
	if a.LeaseExpiresAt != nil {
		t := *a.LeaseExpiresAt
		out.LeaseExpiresAt = &t
	}
	if a.Deadline != nil {
		t := *a.Deadline
		out.Deadline = &t
	}
	return &out
}
