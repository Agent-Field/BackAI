// store.go — persistence layer for suite_crons.
//
// Store wraps a pgxpool and exposes the operations the REST layer and
// the scheduler consume. When pool is nil every method returns
// ErrNotConfigured so the runtime can still boot without a DB.
//
// The table has RLS enabled (see migration 00014_crons.sql). The runtime
// sets app.tenant_id per request via the tenant resolver; the scheduler
// runs under app.bypass_rls=on transactionally so a single goroutine
// can dispatch every tenant's crons.
package crons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the suite_crons accessor.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewStore constructs a Store. pool may be nil — every method then
// returns ErrNotConfigured. log defaults to slog.Default().
func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log}
}

// HasPool reports whether the Store can talk to a database. Used by
// the REST layer for the 503 CRONS_NOT_CONFIGURED branch.
func (s *Store) HasPool() bool {
	return s != nil && s.pool != nil
}

// List returns the crons visible to the caller, optionally filtered by
// tenant. Results are ordered created_at desc so the dashboard renders
// newest-first without client-side sorting.
func (s *Store) List(ctx context.Context, f ListFilters) ([]Cron, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}

	conds := []string{}
	args := []any{}
	tenant := strings.TrimSpace(f.TenantID)
	switch tenant {
	case "":
		// no extra predicate — RLS handles per-request scoping
	case "shared":
		conds = append(conds, "tenant_id is null")
	default:
		args = append(args, tenant)
		conds = append(conds, fmt.Sprintf("(tenant_id = $%d or tenant_id is null)", len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	q := `
		select id, tenant_id, name, job_name, schedule, args,
		       is_active, last_run_at, next_run_at, created_at
		  from suite_crons` + where + `
		 order by created_at desc, id asc`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("crons: list query: %w", err)
	}
	defer rows.Close()

	out := []Cron{}
	for rows.Next() {
		c, err := scanCron(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single cron by id. Returns ErrNotFound if no row
// matches (or if RLS hides it).
func (s *Store) Get(ctx context.Context, id string) (*Cron, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	rows, err := s.pool.Query(ctx, `
		select id, tenant_id, name, job_name, schedule, args,
		       is_active, last_run_at, next_run_at, created_at
		  from suite_crons
		 where id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("crons: get: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanCron(rows)
}

// Create inserts a new cron row. The schedule is parsed and the next
// firing computed so callers don't have to pre-stamp next_run_at.
// IsActive defaults to true when the input pointer is nil.
func (s *Store) Create(ctx context.Context, in CreateInput) (*Cron, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	in.Name = strings.TrimSpace(in.Name)
	in.JobName = strings.TrimSpace(in.JobName)
	in.Schedule = strings.TrimSpace(in.Schedule)
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if in.JobName == "" {
		return nil, fmt.Errorf("%w: job_name is required", ErrInvalidInput)
	}
	if in.Schedule == "" {
		return nil, fmt.Errorf("%w: schedule is required", ErrInvalidInput)
	}
	nextRun, err := NextRun(in.Schedule, time.Now())
	if err != nil {
		return nil, err
	}

	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}

	args := in.Args
	if args == nil {
		args = map[string]any{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("crons: encode args: %w", err)
	}

	var tenantPtr *string
	if in.TenantID != "" {
		t := strings.TrimSpace(in.TenantID)
		if t != "" {
			tenantPtr = &t
		}
	}

	const q = `
		insert into suite_crons
		    (tenant_id, name, job_name, schedule, args, is_active, next_run_at)
		values
		    ($1, $2, $3, $4, $5::jsonb, $6, $7)
		returning id, tenant_id, name, job_name, schedule, args,
		          is_active, last_run_at, next_run_at, created_at`

	rows, err := s.pool.Query(ctx, q,
		tenantPtr, in.Name, in.JobName, in.Schedule, argsJSON, active, nextRun.UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("crons: create: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("crons: create: no row returned")
	}
	return scanCron(rows)
}

// SetActive flips the is_active column. When activating, next_run_at
// is recomputed from the schedule so a long-disabled cron doesn't
// immediately fire a backlog of ticks.
func (s *Store) SetActive(ctx context.Context, id string, active bool) (*Cron, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if id == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	nextRun := time.Time{}
	if active {
		nr, err := NextRun(cur.Schedule, time.Now())
		if err != nil {
			return nil, err
		}
		nextRun = nr.UTC()
	} else {
		// keep existing next_run_at for visibility, but parse to
		// validate the row still has a sane schedule.
		if t, err := time.Parse(time.RFC3339Nano, cur.NextRunAt); err == nil {
			nextRun = t
		} else {
			nextRun = time.Now().Add(24 * time.Hour).UTC()
		}
	}

	const q = `
		update suite_crons
		   set is_active = $2,
		       next_run_at = $3
		 where id = $1
		 returning id, tenant_id, name, job_name, schedule, args,
		           is_active, last_run_at, next_run_at, created_at`

	rows, err := s.pool.Query(ctx, q, id, active, nextRun)
	if err != nil {
		return nil, fmt.Errorf("crons: set active: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanCron(rows)
}

// Delete removes a cron by id. Returns ErrNotFound if no row was
// deleted (caller can use this to distinguish "didn't exist" from a
// successful delete).
func (s *Store) Delete(ctx context.Context, id string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if id == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_crons where id = $1`, id)
	if err != nil {
		return fmt.Errorf("crons: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DueRow is one row in the scheduler's working set. The scheduler
// re-parses the schedule on each tick (cheap; robfig caches nothing
// internally) rather than carrying a parsed cron.Schedule across the
// pool checkout — that would mean either holding the connection
// (forbidden) or copying state per row (no win).
type DueRow struct {
	ID       string
	TenantID *string
	JobName  string
	Schedule string
	Args     json.RawMessage
}

// ClaimDue selects up to `limit` rows that are active and due now,
// taking FOR UPDATE SKIP LOCKED so multiple schedulers in different
// processes never claim the same row. The caller MUST call
// RecordRun for each claimed row inside the same transaction so the
// row's row-lock survives until update.
//
// We run this in one transaction and pull rows into memory; callers
// then iterate, fire each enqueue, and the same transaction commits
// the updates. The transaction commits even if some enqueues fail —
// retrying a missed cron isn't a primitive we expose; the caller
// rolls back only on a programmer error.
func (s *Store) ClaimDue(ctx context.Context, limit int, fn func(ctx context.Context, row DueRow) (recordOK bool, err error)) (int, error) {
	if !s.HasPool() {
		return 0, ErrNotConfigured
	}
	if limit <= 0 {
		limit = 100
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("crons: claim due begin: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Scheduler dispatches across every tenant — bypass RLS so it
	// sees every active row. The scope is the transaction so other
	// queries in the same pool aren't affected.
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return 0, fmt.Errorf("crons: bypass_rls: %w", err)
	}

	rows, err := tx.Query(ctx, `
		select id, tenant_id, job_name, schedule, args
		  from suite_crons
		 where is_active = true
		   and next_run_at <= now()
		 order by next_run_at asc
		 for update skip locked
		 limit $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("crons: claim due query: %w", err)
	}
	working := []DueRow{}
	for rows.Next() {
		var (
			id, jobName, schedule string
			tenantID              *string
			argsRaw               []byte
		)
		if err := rows.Scan(&id, &tenantID, &jobName, &schedule, &argsRaw); err != nil {
			rows.Close()
			return 0, fmt.Errorf("crons: scan due: %w", err)
		}
		working = append(working, DueRow{
			ID:       id,
			TenantID: tenantID,
			JobName:  jobName,
			Schedule: schedule,
			Args:     json.RawMessage(argsRaw),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	processed := 0
	for _, row := range working {
		ok, dispatchErr := fn(ctx, row)
		// Always advance next_run_at — a transient enqueue failure
		// shouldn't make a cron fire twice. The scheduler logs the
		// failure for the operator.
		next, parseErr := NextRun(row.Schedule, now)
		if parseErr != nil {
			s.log.Warn("crons: row has invalid schedule; skipping update",
				"id", row.ID, "schedule", row.Schedule, "error", parseErr)
			continue
		}
		var lastRun any = now
		if !ok {
			lastRun = nil
		}
		if _, err := tx.Exec(ctx, `
			update suite_crons
			   set last_run_at = $2,
			       next_run_at = $3
			 where id = $1
		`, row.ID, lastRun, next); err != nil {
			s.log.Warn("crons: update next_run_at failed",
				"id", row.ID, "error", err)
			continue
		}
		if dispatchErr != nil {
			s.log.Warn("crons: dispatch failed",
				"id", row.ID, "job", row.JobName, "error", dispatchErr)
		}
		processed++
	}

	if err := tx.Commit(ctx); err != nil {
		return processed, fmt.Errorf("crons: claim due commit: %w", err)
	}
	return processed, nil
}

// scanCron reads one suite_crons row into a Cron. Used by every read
// path so the column list stays in lockstep.
func scanCron(rows pgx.Rows) (*Cron, error) {
	var (
		id, name, jobName, schedule string
		tenantID                    *string
		argsRaw                     []byte
		isActive                    bool
		lastRunAt                   *time.Time
		nextRunAt, createdAt        time.Time
	)
	if err := rows.Scan(&id, &tenantID, &name, &jobName, &schedule,
		&argsRaw, &isActive, &lastRunAt, &nextRunAt, &createdAt); err != nil {
		return nil, fmt.Errorf("crons: scan: %w", err)
	}
	args := map[string]any{}
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			// Tolerate non-object args by surfacing an empty map —
			// the dashboard zod schema requires a record.
			args = map[string]any{}
		}
	}
	var lastRunPtr *string
	if lastRunAt != nil {
		s := lastRunAt.UTC().Format(time.RFC3339Nano)
		lastRunPtr = &s
	}
	return &Cron{
		ID:        id,
		TenantID:  tenantID,
		Name:      name,
		JobName:   jobName,
		Schedule:  schedule,
		Args:      args,
		IsActive:  isActive,
		LastRunAt: lastRunPtr,
		NextRunAt: nextRunAt.UTC().Format(time.RFC3339Nano),
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// Sentinel for callers that want to assert "any DB error".
var ErrDB = errors.New("crons: database error")
