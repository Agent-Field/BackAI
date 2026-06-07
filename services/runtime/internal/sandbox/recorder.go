// SPDX-License-Identifier: Apache-2.0

// recorder.go — persistence layer for suite_sandbox_runs.
//
// Three write paths:
//
//   - Insert      : a new run is starting; row goes in at status='queued'
//                   with the static fields (tenant_id, workspace_id,
//                   adapter, image, command) populated and every
//                   accounting column (exit_code, cpu_seconds, urls,
//                   started_at, ended_at) NULL.
//   - UpdateStatus: lifecycle transition (queued -> running, etc.)
//                   without final accounting yet. Used by the adapter
//                   the moment the container starts.
//   - UpdateFinal : terminal transition. Writes status + exit_code +
//                   duration + cpu_seconds + memory_peak + network +
//                   cost_usd + stdout_url + stderr_url + artifacts_url
//                   + started_at + ended_at in a single statement.
//
// The Pool may be nil — in that case every method returns
// (zero, ErrNotConfigured) and the Service short-circuits accordingly.
// This keeps the runtime boot path free of "if cfg.DB != nil" branches.
package sandbox

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

// Recorder writes + reads suite_sandbox_runs rows.
type Recorder struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewRecorder constructs a Recorder. pool may be nil; log defaults to
// slog.Default().
func NewRecorder(pool *pgxpool.Pool, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	return &Recorder{pool: pool, log: log}
}

// HasPool reports whether the Recorder can talk to a database. Used by
// the Service for the "no DB" graceful-degrade path.
func (r *Recorder) HasPool() bool {
	return r != nil && r.pool != nil
}

// Insert writes a fresh row at status='queued' and returns the rendered
// SandboxRun (with the generated id + created_at). Most fields are NULL
// at this stage.
func (r *Recorder) Insert(ctx context.Context, spec RunSpec, adapter string) (*SandboxRun, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	cmdJSON, err := json.Marshal(spec.Command)
	if err != nil {
		return nil, fmt.Errorf("sandbox: encode command: %w", err)
	}
	var (
		tenantPtr, workspacePtr *string
	)
	if spec.TenantID != "" {
		t := spec.TenantID
		tenantPtr = &t
	}
	if spec.WorkspaceID != "" {
		w := spec.WorkspaceID
		workspacePtr = &w
	}

	const q = `
		insert into suite_sandbox_runs
		    (tenant_id, workspace_id, adapter, image, command, status)
		values ($1, $2, $3, $4, $5, 'queued')
		returning id, created_at
	`
	var (
		id        string
		createdAt time.Time
	)
	if err := r.pool.QueryRow(ctx, q,
		tenantPtr, workspacePtr, adapter, spec.Image, cmdJSON,
	).Scan(&id, &createdAt); err != nil {
		return nil, fmt.Errorf("sandbox: insert run: %w", err)
	}

	run := &SandboxRun{
		ID:        id,
		TenantID:  tenantPtr,
		WorkspaceID: workspacePtr,
		Adapter:   adapter,
		Image:     spec.Image,
		Command:   append([]string{}, spec.Command...),
		Status:    StatusQueued,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
	}
	return run, nil
}

// UpdateStatus mutates only the status column. Used for non-terminal
// transitions (queued -> running, primarily). started_at is also set on
// the queued -> running transition since the adapter already knows the
// wall-clock at that moment.
func (r *Recorder) UpdateStatus(ctx context.Context, id string, status Status, startedAt *time.Time) error {
	if !r.HasPool() {
		return ErrNotConfigured
	}
	if startedAt != nil {
		_, err := r.pool.Exec(ctx, `
			update suite_sandbox_runs
			   set status = $2, started_at = coalesce(started_at, $3)
			 where id = $1
		`, id, string(status), startedAt.UTC())
		return err
	}
	_, err := r.pool.Exec(ctx, `
		update suite_sandbox_runs
		   set status = $2
		 where id = $1
	`, id, string(status))
	return err
}

// UpdateFinal writes the terminal accounting columns + status in one
// statement. Called by the Service after the adapter returns.
func (r *Recorder) UpdateFinal(
	ctx context.Context,
	id string,
	status Status,
	res RunResult,
	costUSD float64,
) error {
	if !r.HasPool() {
		return ErrNotConfigured
	}
	var (
		exitCodePtr  *int
		durationPtr  *int
		cpuPtr       *float64
		memoryPtr    *int
		netInPtr     *int64
		netOutPtr    *int64
		costPtr      *float64
		stdoutPtr    *string
		stderrPtr    *string
		artifactsPtr *string
		startedPtr   *time.Time
		endedPtr     *time.Time
	)
	exitCodePtr = &res.ExitCode
	durationPtr = &res.DurationS
	if res.CPUSeconds > 0 {
		v := res.CPUSeconds
		cpuPtr = &v
	}
	if res.MemoryPeakMB > 0 {
		v := res.MemoryPeakMB
		memoryPtr = &v
	}
	if res.NetworkBytesIn > 0 {
		v := res.NetworkBytesIn
		netInPtr = &v
	}
	if res.NetworkBytesOut > 0 {
		v := res.NetworkBytesOut
		netOutPtr = &v
	}
	if costUSD > 0 {
		v := costUSD
		costPtr = &v
	}
	if res.StdoutURL != "" {
		v := res.StdoutURL
		stdoutPtr = &v
	}
	if res.StderrURL != "" {
		v := res.StderrURL
		stderrPtr = &v
	}
	if res.ArtifactsURL != "" {
		v := res.ArtifactsURL
		artifactsPtr = &v
	}
	if !res.StartedAt.IsZero() {
		t := res.StartedAt.UTC()
		startedPtr = &t
	}
	if !res.EndedAt.IsZero() {
		t := res.EndedAt.UTC()
		endedPtr = &t
	}

	_, err := r.pool.Exec(ctx, `
		update suite_sandbox_runs
		   set status            = $2,
		       exit_code         = $3,
		       duration_s        = $4,
		       cpu_seconds       = $5,
		       memory_peak_mb    = $6,
		       network_bytes_in  = $7,
		       network_bytes_out = $8,
		       cost_usd          = $9,
		       stdout_url        = $10,
		       stderr_url        = $11,
		       artifacts_url     = $12,
		       started_at        = coalesce($13, started_at),
		       ended_at          = $14
		 where id = $1
	`,
		id, string(status),
		exitCodePtr, durationPtr, cpuPtr, memoryPtr,
		netInPtr, netOutPtr, costPtr,
		stdoutPtr, stderrPtr, artifactsPtr,
		startedPtr, endedPtr,
	)
	if err != nil {
		return fmt.Errorf("sandbox: update final: %w", err)
	}
	return nil
}

// Get fetches a single row by id. Returns ErrNotFound when no row
// matches.
func (r *Recorder) Get(ctx context.Context, id string) (*SandboxRun, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	rows, err := r.pool.Query(ctx, selectColumns+` from suite_sandbox_runs where id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("sandbox: select run: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	run, err := scanRow(rows)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// List returns one page + total. The default sort is created_at desc so
// the dashboard renders "newest first" without additional client work.
func (r *Recorder) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if !r.HasPool() {
		// Tolerant: return empty so /api/v1/sandbox/runs renders an
		// empty table rather than 503.
		return &ListResult{Runs: []SandboxRun{}}, nil
	}
	if f.Limit < 1 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	conds := []string{}
	args := []any{}
	if f.TenantID != "" {
		args = append(args, f.TenantID)
		conds = append(conds, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}

	// Total first (small, indexed scan). Then paginated rows.
	var total int
	if err := r.pool.QueryRow(ctx,
		"select count(*) from suite_sandbox_runs"+where,
		args...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("sandbox: count runs: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := selectColumns + " from suite_sandbox_runs" + where +
		fmt.Sprintf(" order by created_at desc limit $%d offset $%d", len(args)-1, len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sandbox: list runs: %w", err)
	}
	defer rows.Close()

	out := make([]SandboxRun, 0, f.Limit)
	for rows.Next() {
		run, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ListResult{
		Runs:    out,
		Total:   total,
		HasMore: f.Offset+len(out) < total,
	}, nil
}

// PoolStats returns the aggregates used by GET /api/v1/sandbox/pool.
// today is the local-day boundary the caller picks; using a parameter
// keeps the recorder timezone-agnostic.
func (r *Recorder) PoolStats(ctx context.Context, today time.Time) (active, queued, totalToday int, cpuSecondsToday, costUSDToday float64, err error) {
	if !r.HasPool() {
		return 0, 0, 0, 0, 0, nil
	}
	// Live counters.
	err = r.pool.QueryRow(ctx, `
		select
		  coalesce(sum(case when status = 'running' then 1 else 0 end), 0),
		  coalesce(sum(case when status = 'queued' then 1 else 0 end), 0)
		from suite_sandbox_runs
		where status in ('running', 'queued')
	`).Scan(&active, &queued)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("sandbox: live counters: %w", err)
	}

	// Today's totals (anything created today, including in-flight).
	err = r.pool.QueryRow(ctx, `
		select
		  count(*),
		  coalesce(sum(cpu_seconds), 0)::float8,
		  coalesce(sum(cost_usd), 0)::float8
		from suite_sandbox_runs
		where created_at >= $1
	`, today.UTC()).Scan(&totalToday, &cpuSecondsToday, &costUSDToday)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("sandbox: today totals: %w", err)
	}
	return active, queued, totalToday, cpuSecondsToday, costUSDToday, nil
}

// ─── internals ────────────────────────────────────────────────────────────

// selectColumns is the column list used by Get + List so the scanner
// can rely on a stable order.
const selectColumns = `
	select id, tenant_id, workspace_id, adapter, image, command,
	       status, exit_code, duration_s, cpu_seconds, memory_peak_mb,
	       network_bytes_in, network_bytes_out, cost_usd,
	       stdout_url, stderr_url, artifacts_url,
	       started_at, ended_at, created_at
`

// scanRow consumes one pgx.Rows position into a *SandboxRun. Pointer
// columns map directly to the *T fields on SandboxRun so NULL stays
// NULL.
func scanRow(rows pgx.Rows) (*SandboxRun, error) {
	var (
		id              string
		tenantID        *string
		workspaceID     *string
		adapter         string
		image           string
		commandJSON     []byte
		status          string
		exitCode        *int
		durationS       *int
		cpuSeconds      *float64
		memoryPeakMB    *int
		networkBytesIn  *int64
		networkBytesOut *int64
		costUSD         *float64
		stdoutURL       *string
		stderrURL       *string
		artifactsURL    *string
		startedAt       *time.Time
		endedAt         *time.Time
		createdAt       time.Time
	)
	if err := rows.Scan(
		&id, &tenantID, &workspaceID, &adapter, &image, &commandJSON,
		&status, &exitCode, &durationS, &cpuSeconds, &memoryPeakMB,
		&networkBytesIn, &networkBytesOut, &costUSD,
		&stdoutURL, &stderrURL, &artifactsURL,
		&startedAt, &endedAt, &createdAt,
	); err != nil {
		return nil, fmt.Errorf("sandbox: scan row: %w", err)
	}
	var command []string
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &command); err != nil {
			return nil, fmt.Errorf("sandbox: decode command: %w", err)
		}
	}
	run := &SandboxRun{
		ID:              id,
		TenantID:        tenantID,
		WorkspaceID:     workspaceID,
		Adapter:         adapter,
		Image:           image,
		Command:         command,
		Status:          Status(status),
		ExitCode:        exitCode,
		DurationS:       durationS,
		CPUSeconds:      cpuSeconds,
		MemoryPeakMB:    memoryPeakMB,
		NetworkBytesIn:  networkBytesIn,
		NetworkBytesOut: networkBytesOut,
		CostUSD:         costUSD,
		StdoutURL:       stdoutURL,
		StderrURL:       stderrURL,
		ArtifactsURL:    artifactsURL,
		CreatedAt:       createdAt.UTC().Format(time.RFC3339Nano),
	}
	if startedAt != nil {
		s := startedAt.UTC().Format(time.RFC3339Nano)
		run.StartedAt = &s
	}
	if endedAt != nil {
		s := endedAt.UTC().Format(time.RFC3339Nano)
		run.EndedAt = &s
	}
	if run.Command == nil {
		run.Command = []string{}
	}
	return run, nil
}

// Guard against an accidental dead-code removal of pgx import — the
// scanner relies on rows.Scan from pgx.Rows.
var _ = errors.Is