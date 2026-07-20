// SPDX-License-Identifier: Apache-2.0

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgRemoteStore is the production RemoteStore, backed by suite_job_leases /
// suite_job_worker_logs. Tenant isolation holds two ways:
//
//   - every statement carries an explicit tenant_id predicate, and
//   - FORCE ROW LEVEL SECURITY on both tables gates on app.tenant_id, which
//     the pool's PrepareConn hook binds from the request/executor context.
//
// The two are redundant on purpose: a bug in one layer can't leak across
// tenants.
type pgRemoteStore struct {
	pool *pgxpool.Pool
}

func newPGRemoteStore(pool *pgxpool.Pool) *pgRemoteStore {
	if pool == nil {
		return nil
	}
	return &pgRemoteStore{pool: pool}
}

// leaseCols is the canonical projection used by every scan.
const leaseCols = `tenant_id::text, job_id, attempt, kind, payload, state,
	coalesce(worker_id, ''), lease_expires_at, deadline, result,
	coalesce(error, ''), retryable, canceled, created_at, updated_at`

func scanLease(row pgx.Row) (*RemoteAttempt, error) {
	var (
		a        RemoteAttempt
		payload  []byte
		result   []byte
		leaseExp *time.Time
		deadline *time.Time
		stateStr string
	)
	if err := row.Scan(
		&a.TenantID, &a.JobID, &a.Attempt, &a.Kind, &payload, &stateStr,
		&a.WorkerID, &leaseExp, &deadline, &result,
		&a.Error, &a.Retryable, &a.Canceled, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	a.State = AttemptState(stateStr)
	if len(payload) > 0 {
		a.Payload = json.RawMessage(payload)
	} else {
		a.Payload = json.RawMessage("{}")
	}
	if len(result) > 0 {
		a.Result = json.RawMessage(result)
	}
	a.LeaseExpiresAt = leaseExp
	a.Deadline = deadline
	return &a, nil
}

func (s *pgRemoteStore) EnqueueAttempt(ctx context.Context, att RemoteAttempt) (*RemoteAttempt, error) {
	tenantID := att.TenantID
	if tenantID == "" {
		tenantID = defaultTenantID
	}
	payload := att.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs: enqueue attempt begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Supersede older non-terminal attempts of the same job so a stale
	// attempt can't still be leased after a River retry landed a newer one.
	if _, err := tx.Exec(ctx, `
		update suite_job_leases
		   set state = 'superseded', updated_at = now()
		 where tenant_id = $1 and job_id = $2 and attempt < $3
		   and state in ('ready', 'leased')
	`, tenantID, att.JobID, att.Attempt); err != nil {
		return nil, fmt.Errorf("jobs: supersede older attempts: %w", err)
	}

	// Register the attempt. Idempotent: a re-run of the same (job, attempt)
	// leaves an in-flight lease / result untouched.
	if _, err := tx.Exec(ctx, `
		insert into suite_job_leases
			(tenant_id, job_id, attempt, kind, payload, state, deadline, created_at, updated_at)
		values ($1, $2, $3, $4, $5, 'ready', $6, now(), now())
		on conflict (job_id, attempt) do nothing
	`, tenantID, att.JobID, att.Attempt, att.Kind, payload, att.Deadline); err != nil {
		return nil, fmt.Errorf("jobs: insert attempt: %w", err)
	}

	cur, err := scanLease(tx.QueryRow(ctx, `select `+leaseCols+`
		from suite_job_leases where tenant_id = $1 and job_id = $2 and attempt = $3`,
		tenantID, att.JobID, att.Attempt))
	if err != nil {
		return nil, fmt.Errorf("jobs: read attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("jobs: enqueue attempt commit: %w", err)
	}
	return cur, nil
}

func (s *pgRemoteStore) Lease(ctx context.Context, req LeaseRequest) (*RemoteAttempt, error) {
	if len(req.Kinds) == 0 {
		return nil, nil
	}
	cur, err := scanLease(s.pool.QueryRow(ctx, `
		with picked as (
			select job_id, attempt
			  from suite_job_leases
			 where tenant_id = $1 and state = 'ready' and kind = any($4::text[])
			 order by created_at asc
			 for update skip locked
			 limit 1
		)
		update suite_job_leases l
		   set state = 'leased', worker_id = $2,
		       lease_expires_at = now() + make_interval(secs => $3),
		       updated_at = now()
		  from picked
		 where l.job_id = picked.job_id and l.attempt = picked.attempt
		returning `+prefixCols("l.")+``,
		req.TenantID, req.WorkerID, req.TTL.Seconds(), req.Kinds))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("jobs: lease: %w", err)
	}
	return cur, nil
}

func (s *pgRemoteStore) Heartbeat(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, ttl time.Duration) (*RemoteAttempt, error) {
	cur, err := scanLease(s.pool.QueryRow(ctx, `
		update suite_job_leases
		   set lease_expires_at = now() + make_interval(secs => $5), updated_at = now()
		 where tenant_id = $1 and job_id = $2 and attempt = $3
		   and state = 'leased' and worker_id = $4
		returning `+leaseCols+``,
		tenantID, jobID, attempt, workerID, ttl.Seconds()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.classifyMiss(ctx, tenantID, jobID, attempt, workerID)
		}
		return nil, fmt.Errorf("jobs: heartbeat: %w", err)
	}
	return cur, nil
}

func (s *pgRemoteStore) Complete(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, result json.RawMessage) error {
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	tag, err := s.pool.Exec(ctx, `
		update suite_job_leases
		   set state = 'completed', result = $5, lease_expires_at = null, updated_at = now()
		 where tenant_id = $1 and job_id = $2 and attempt = $3
		   and state = 'leased' and worker_id = $4
	`, tenantID, jobID, attempt, workerID, result)
	if err != nil {
		return fmt.Errorf("jobs: complete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.classifyMiss(ctx, tenantID, jobID, attempt, workerID)
	}
	return nil
}

func (s *pgRemoteStore) Fail(ctx context.Context, tenantID string, jobID int64, attempt int, workerID string, errMsg string, retryable bool) error {
	tag, err := s.pool.Exec(ctx, `
		update suite_job_leases
		   set state = 'failed', error = $5, retryable = $6, lease_expires_at = null, updated_at = now()
		 where tenant_id = $1 and job_id = $2 and attempt = $3
		   and state = 'leased' and worker_id = $4
	`, tenantID, jobID, attempt, workerID, errMsg, retryable)
	if err != nil {
		return fmt.Errorf("jobs: fail: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s.classifyMiss(ctx, tenantID, jobID, attempt, workerID)
	}
	return nil
}

func (s *pgRemoteStore) GetAttempt(ctx context.Context, tenantID string, jobID int64, attempt int) (*RemoteAttempt, error) {
	cur, err := scanLease(s.pool.QueryRow(ctx, `select `+leaseCols+`
		from suite_job_leases where tenant_id = $1 and job_id = $2 and attempt = $3`,
		tenantID, jobID, attempt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAttemptNotFound
		}
		return nil, fmt.Errorf("jobs: get attempt: %w", err)
	}
	return cur, nil
}

func (s *pgRemoteStore) MarkCanceled(ctx context.Context, tenantID string, jobID int64) error {
	tag, err := s.pool.Exec(ctx, `
		update suite_job_leases
		   set canceled = true, updated_at = now()
		 where tenant_id = $1 and job_id = $2
		   and state not in ('completed', 'failed', 'superseded')
	`, tenantID, jobID)
	if err != nil {
		return fmt.Errorf("jobs: mark canceled: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAttemptNotFound
	}
	return nil
}

func (s *pgRemoteStore) AppendLogs(ctx context.Context, tenantID string, jobID int64, attempt int, lines []LogLine) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	// Guard: the attempt must exist for this tenant before we attach logs.
	if _, err := s.GetAttempt(ctx, tenantID, jobID, attempt); err != nil {
		return 0, err
	}
	batch := &pgx.Batch{}
	for _, ln := range lines {
		level := ln.Level
		if level == "" {
			level = "info"
		}
		fields := ln.Fields
		if len(fields) == 0 {
			fields = json.RawMessage("{}")
		}
		at := time.Now()
		if ln.At != nil {
			at = *ln.At
		}
		batch.Queue(`
			insert into suite_job_worker_logs
				(tenant_id, job_id, attempt, level, message, fields, at)
			values ($1, $2, $3, $4, $5, $6, $7)
		`, tenantID, jobID, attempt, level, ln.Message, fields, at)
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range lines {
		if _, err := br.Exec(); err != nil {
			return 0, fmt.Errorf("jobs: append logs: %w", err)
		}
	}
	return len(lines), nil
}

// classifyMiss turns a 0-row write into the right sentinel: not-found,
// already-resolved, or leased-by-someone-else / not-leased.
func (s *pgRemoteStore) classifyMiss(ctx context.Context, tenantID string, jobID int64, attempt int, _ string) error {
	var stateStr string
	err := s.pool.QueryRow(ctx, `
		select state from suite_job_leases
		 where tenant_id = $1 and job_id = $2 and attempt = $3
	`, tenantID, jobID, attempt).Scan(&stateStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAttemptNotFound
		}
		return fmt.Errorf("jobs: classify miss: %w", err)
	}
	if AttemptState(stateStr).Terminal() {
		return ErrAttemptResolved
	}
	return ErrAttemptNotLeased
}

// prefixCols rewrites leaseCols for a table alias used in a RETURNING that
// references the aliased target (e.g. `l.`). Only the bare column names are
// prefixed; the cast/coalesce expressions already reference bare names.
func prefixCols(prefix string) string {
	return prefix + `tenant_id::text, ` + prefix + `job_id, ` + prefix + `attempt, ` +
		prefix + `kind, ` + prefix + `payload, ` + prefix + `state, ` +
		`coalesce(` + prefix + `worker_id, ''), ` + prefix + `lease_expires_at, ` +
		prefix + `deadline, ` + prefix + `result, coalesce(` + prefix + `error, ''), ` +
		prefix + `retryable, ` + prefix + `canceled, ` + prefix + `created_at, ` + prefix + `updated_at`
}
