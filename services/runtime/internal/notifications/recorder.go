// SPDX-License-Identifier: Apache-2.0

// recorder.go — persistence layer for suite_notifications.
//
// Four write paths:
//
//   - Insert      : a new notification queued; row goes in at
//     status='queued' with adapter NULL (the worker sets
//     it when it picks the row up).
//   - UpdateStatus: lifecycle transition (queued -> sending, sending ->
//     sent/failed). Touches status, provider_message_id,
//     last_error, attempts, sent_at as appropriate.
//   - ClaimQueued : atomic worker drain: marks up to N queued rows
//     (whose scheduled_at <= now()) as 'sending' and
//     returns their bodies. Concurrent worker pods cannot
//     double-process a row thanks to the FOR UPDATE SKIP
//     LOCKED predicate.
//   - Stats       : aggregate counters for the dashboard KPI strip.
//
// The Pool may be nil — every method returns (zero, ErrNotConfigured)
// and the Service short-circuits. Keeps boot path branch-free.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Recorder writes + reads suite_notifications rows.
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
// Notification (with the generated id + created_at).
func (r *Recorder) Insert(ctx context.Context, in SendInput) (*Notification, error) {
	return r.insert(ctx, in, StatusQueued, "")
}

func (r *Recorder) InsertSkipped(ctx context.Context, in SendInput, reason string) (*Notification, error) {
	return r.insert(ctx, in, StatusSkipped, reason)
}

func (r *Recorder) insert(ctx context.Context, in SendInput, status Status, lastError string) (*Notification, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	// Bind the row's tenant so the RLS policy on suite_notifications
	// passes on the bare pool. Empty is fine (NULL-tenant rows pass).
	ctx = tenantctx.WithTenant(ctx, in.TenantID, "")
	dataJSON, err := json.Marshal(in.Data)
	if err != nil {
		return nil, fmt.Errorf("notifications: encode data: %w", err)
	}
	var (
		tenantPtr  *string
		fromPtr    *string
		subjectPtr *string
		schedPtr   *time.Time
	)
	if in.TenantID != "" {
		t := in.TenantID
		tenantPtr = &t
	}
	if in.From != "" {
		f := in.From
		fromPtr = &f
	}
	if in.Subject != "" {
		s := in.Subject
		subjectPtr = &s
	}
	if in.ScheduledAt != nil {
		t := in.ScheduledAt.UTC()
		schedPtr = &t
	}

	const q = `
		insert into suite_notifications
		    (tenant_id, kind, template, "to", "from", subject, data, scheduled_at, status, last_error)
		values ($1, $2, $3, $4, $5, $6, $7, coalesce($8, now()), $9, nullif($10, ''))
		returning id, scheduled_at, created_at
	`
	var (
		id          string
		scheduledAt time.Time
		createdAt   time.Time
	)
	if err := r.pool.QueryRow(ctx, q,
		tenantPtr, string(in.Kind), in.Template, in.To,
		fromPtr, subjectPtr, dataJSON, schedPtr, string(status), lastError,
	).Scan(&id, &scheduledAt, &createdAt); err != nil {
		return nil, fmt.Errorf("notifications: insert: %w", err)
	}

	return &Notification{
		ID:          id,
		TenantID:    tenantPtr,
		Kind:        in.Kind,
		Adapter:     nil,
		Template:    in.Template,
		To:          in.To,
		From:        fromPtr,
		Subject:     subjectPtr,
		Data:        cloneData(in.Data),
		Status:      status,
		Attempts:    0,
		LastError:   nullableString(lastError),
		ScheduledAt: scheduledAt.UTC().Format(time.RFC3339Nano),
		CreatedAt:   createdAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ListMutes returns active and expired mute rows ordered newest-first.
func (r *Recorder) ListMutes(ctx context.Context, tenantID string) (*MuteListResult, error) {
	if !r.HasPool() {
		return &MuteListResult{Mutes: []Mute{}}, nil
	}
	// Bind the requested tenant so RLS on suite_notification_mutes passes.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	args := []any{}
	where := ""
	if tenantID != "" {
		args = append(args, tenantID)
		where = " where tenant_id = $1 or tenant_id is null"
	}
	rows, err := r.pool.Query(ctx, `
		select id, tenant_id, kind, recipient, template, category,
		       reason, expires_at, created_by, created_at
		  from suite_notification_mutes`+where+`
		 order by created_at desc
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("notifications: list mutes: %w", err)
	}
	defer rows.Close()
	out := []Mute{}
	for rows.Next() {
		m, err := scanMute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &MuteListResult{Mutes: out}, nil
}

func (r *Recorder) CreateMute(ctx context.Context, in CreateMuteInput) (*Mute, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	// Bind the mute's tenant (trimmed, to match the value we store) so
	// the RLS insert policy on suite_notification_mutes passes.
	ctx = tenantctx.WithTenant(ctx, strings.TrimSpace(in.TenantID), "")
	if err := in.Pattern.Validate(); err != nil {
		return nil, err
	}
	var tenantPtr, reasonPtr, createdByPtr *string
	if strings.TrimSpace(in.TenantID) != "" {
		t := strings.TrimSpace(in.TenantID)
		tenantPtr = &t
	}
	if strings.TrimSpace(in.Reason) != "" {
		reason := strings.TrimSpace(in.Reason)
		reasonPtr = &reason
	}
	if strings.TrimSpace(in.CreatedBy) != "" {
		createdBy := strings.TrimSpace(in.CreatedBy)
		createdByPtr = &createdBy
	}
	rows, err := r.pool.Query(ctx, `
		insert into suite_notification_mutes
		    (tenant_id, kind, recipient, template, category, reason, expires_at, created_by)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning id, tenant_id, kind, recipient, template, category,
		          reason, expires_at, created_by, created_at
	`, tenantPtr, in.Pattern.Kind, in.Pattern.Recipient, in.Pattern.Template,
		in.Pattern.Category, reasonPtr, in.ExpiresAt, createdByPtr)
	if err != nil {
		return nil, fmt.Errorf("notifications: create mute: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("notifications: create mute: no row returned")
	}
	return scanMute(rows)
}

func (r *Recorder) DeleteMute(ctx context.Context, id string) error {
	if !r.HasPool() {
		return ErrNotConfigured
	}
	// Delete-by-id has no tenant in scope (operator path). Run under a
	// bypass-RLS tx so the FORCE-RLS policy on suite_notification_mutes
	// does not silently drop the row.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notifications: delete mute begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return fmt.Errorf("notifications: delete mute bypass: %w", err)
	}
	tag, err := tx.Exec(ctx, `delete from suite_notification_mutes where id = $1`, id)
	if err != nil {
		return fmt.Errorf("notifications: delete mute: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("notifications: delete mute commit: %w", err)
	}
	return nil
}

func (r *Recorder) ListChannels(ctx context.Context) ([]Channel, error) {
	if !r.HasPool() {
		return []Channel{}, nil
	}
	rows, err := r.pool.Query(ctx, `
        select id::text, kind, config_json, enabled, created_at, updated_at
        from suite_notification_channels
        order by kind asc
    `)
	if err != nil {
		return nil, fmt.Errorf("notifications: list channels: %w", err)
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		ch.Source = "db"
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Recorder) UpsertChannel(ctx context.Context, in ChannelInput) (Channel, error) {
	if !r.HasPool() {
		return Channel{}, ErrNotConfigured
	}
	raw, err := json.Marshal(in.Config)
	if err != nil {
		return Channel{}, fmt.Errorf("%w: config must be JSON-serializable", ErrInvalidInput)
	}
	rows, err := r.pool.Query(ctx, `
        insert into suite_notification_channels (kind, config_json, enabled, created_at, updated_at)
        values ($1, $2, $3, now(), now())
        on conflict (kind) do update set
          config_json = excluded.config_json,
          enabled = excluded.enabled,
          updated_at = now()
        returning id::text, kind, config_json, enabled, created_at, updated_at
    `, string(in.Kind), raw, in.Enabled)
	if err != nil {
		return Channel{}, fmt.Errorf("notifications: upsert channel: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return Channel{}, fmt.Errorf("notifications: upsert channel: no row returned")
	}
	ch, err := scanChannel(rows)
	if err != nil {
		return Channel{}, err
	}
	ch.Source = "db"
	return ch, nil
}

func (r *Recorder) DeleteChannel(ctx context.Context, id, kind string) error {
	if !r.HasPool() {
		return ErrNotConfigured
	}
	var tag pgconn.CommandTag
	var err error
	if strings.TrimSpace(id) != "" {
		tag, err = r.pool.Exec(ctx, `delete from suite_notification_channels where id = $1`, strings.TrimSpace(id))
	} else {
		tag, err = r.pool.Exec(ctx, `delete from suite_notification_channels where kind = $1`, strings.TrimSpace(kind))
	}
	if err != nil {
		return fmt.Errorf("notifications: delete channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Recorder) MatchingMute(ctx context.Context, in SendInput) (*Mute, error) {
	if !r.HasPool() {
		return nil, nil
	}
	// Bind the send's tenant so RLS on suite_notification_mutes passes.
	// Empty means an untenanted send, which only matches global (NULL)
	// mutes — exactly what the query predicate intends.
	ctx = tenantctx.WithTenant(ctx, in.TenantID, "")
	category := ""
	if v, ok := in.Data["category"].(string); ok {
		category = strings.TrimSpace(v)
	}
	if category == "" {
		category = "*"
	}
	var tenant any
	if in.TenantID != "" {
		tenant = in.TenantID
	}
	rows, err := r.pool.Query(ctx, `
		select id, tenant_id, kind, recipient, template, category,
		       reason, expires_at, created_by, created_at
		  from suite_notification_mutes
		 where ($1::uuid is null or tenant_id is null or tenant_id = $1::uuid)
		   and (expires_at is null or expires_at > now())
		   and (kind = '*' or kind = $2)
		   and (recipient = '*' or recipient = $3)
		   and (template = '*' or template = $4)
		   and (category = '*' or category = $5)
		 order by tenant_id nulls last, created_at desc
		 limit 1
	`, tenant, string(in.Kind), in.To, in.Template, category)
	if err != nil {
		return nil, fmt.Errorf("notifications: matching mute: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return scanMute(rows)
}

func scanMute(rows pgx.Rows) (*Mute, error) {
	var (
		id, kind, recipient, template, category string
		tenantID, reason, createdBy             *string
		expiresAt                               *time.Time
		createdAt                               time.Time
	)
	if err := rows.Scan(&id, &tenantID, &kind, &recipient, &template, &category,
		&reason, &expiresAt, &createdBy, &createdAt); err != nil {
		return nil, fmt.Errorf("notifications: scan mute: %w", err)
	}
	var expires *string
	if expiresAt != nil {
		s := expiresAt.UTC().Format(time.RFC3339Nano)
		expires = &s
	}
	return &Mute{
		ID:       id,
		TenantID: tenantID,
		Pattern: MutePattern{
			Kind:      kind,
			Recipient: recipient,
			Template:  template,
			Category:  category,
		},
		Reason:    reason,
		ExpiresAt: expires,
		CreatedBy: createdBy,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func scanChannel(rows pgx.Rows) (Channel, error) {
	var (
		ch                   Channel
		kind                 string
		raw                  []byte
		createdAt, updatedAt time.Time
	)
	if err := rows.Scan(&ch.ID, &kind, &raw, &ch.Enabled, &createdAt, &updatedAt); err != nil {
		return Channel{}, fmt.Errorf("notifications: scan channel: %w", err)
	}
	ch.Kind = Kind(kind)
	ch.Config = decodeMap(raw)
	ch.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	ch.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return ch, nil
}

// UpdateStatus writes a lifecycle transition. providerMessageID and
// errMsg are optional (empty strings mean "don't touch / NULL"). When
// status is StatusSent we also stamp sent_at = now(). When status is
// StatusSending we set adapter so the dashboard sees who is handling
// the row.
func (r *Recorder) UpdateStatus(
	ctx context.Context,
	id string,
	status Status,
	adapter string,
	providerMessageID string,
	errMsg string,
) error {
	if !r.HasPool() {
		return ErrNotConfigured
	}
	var (
		adapterPtr   *string
		providerPtr  *string
		errMsgPtr    *string
		setSentAt    bool
		bumpAttempts bool
	)
	if adapter != "" {
		a := adapter
		adapterPtr = &a
	}
	if providerMessageID != "" {
		p := providerMessageID
		providerPtr = &p
	}
	if errMsg != "" {
		e := errMsg
		errMsgPtr = &e
	}
	switch status {
	case StatusSent:
		setSentAt = true
		bumpAttempts = true
	case StatusFailed:
		bumpAttempts = true
	case StatusSending:
		bumpAttempts = true
	}
	// Lifecycle updates are keyed by id from the worker, which does not
	// know the row's tenant. Run under a bypass-RLS tx so the FORCE-RLS
	// policy on suite_notifications does not silently no-op the update.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notifications: update status begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return fmt.Errorf("notifications: update status bypass: %w", err)
	}
	_, err = tx.Exec(ctx, `
		update suite_notifications
		   set status              = $2,
		       adapter             = coalesce($3, adapter),
		       provider_message_id = coalesce($4, provider_message_id),
		       last_error          = case when $5::text is not null then $5 else last_error end,
		       attempts            = case when $6 then attempts + 1 else attempts end,
		       sent_at             = case when $7 then now() else sent_at end
		 where id = $1
	`, id, string(status), adapterPtr, providerPtr, errMsgPtr, bumpAttempts, setSentAt)
	if err != nil {
		return fmt.Errorf("notifications: update status: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("notifications: update status commit: %w", err)
	}
	return nil
}

// ClaimQueued atomically claims up to `limit` queued rows whose
// scheduled_at <= now() and marks them 'sending'. Returns the claimed
// rows so the worker can dispatch them through the adapter.
//
// Uses `for update skip locked` so multiple worker pods race safely:
// the first transaction to lock a row wins, others move on to the
// next.
func (r *Recorder) ClaimQueued(ctx context.Context, adapter string, limit int) ([]Notification, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	if limit <= 0 {
		limit = MaxBatchSize
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: claim begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The worker drains queued rows across ALL tenants, so there is no
	// single tenant to bind. Bypass RLS for this cross-tenant sweep.
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return nil, fmt.Errorf("notifications: claim bypass: %w", err)
	}

	// The CTE locks rows with FOR UPDATE SKIP LOCKED so concurrent
	// workers race safely. The UPDATE then flips status to 'sending',
	// stamps adapter (when not already set), and bumps attempts. The
	// RETURNING clause feeds the scanner so we can dispatch without a
	// second round-trip.
	const claimQ = `
		with claimed as (
		    select id from suite_notifications
		     where status = 'queued' and scheduled_at <= now()
		     order by scheduled_at asc
		     for update skip locked
		     limit $1
		)
		update suite_notifications
		   set status   = 'sending',
		       adapter  = coalesce(adapter, $2),
		       attempts = attempts + 1
		  from claimed
		 where suite_notifications.id = claimed.id
		returning suite_notifications.id, suite_notifications.tenant_id,
		          suite_notifications.kind, suite_notifications.adapter,
		          suite_notifications.template, suite_notifications."to",
		          suite_notifications."from", suite_notifications.subject,
		          suite_notifications.data, suite_notifications.status,
		          suite_notifications.provider_message_id,
		          suite_notifications.attempts, suite_notifications.last_error,
		          suite_notifications.scheduled_at, suite_notifications.sent_at,
		          suite_notifications.created_at`

	rows, err := tx.Query(ctx, claimQ, limit, adapter)
	if err != nil {
		return nil, fmt.Errorf("notifications: claim query: %w", err)
	}
	defer rows.Close()

	out := make([]Notification, 0, limit)
	for rows.Next() {
		n, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("notifications: claim commit: %w", err)
	}
	return out, nil
}

// Get fetches a single row by id. Returns ErrNotFound when no row
// matches.
func (r *Recorder) Get(ctx context.Context, id string) (*Notification, error) {
	if !r.HasPool() {
		return nil, ErrNotConfigured
	}
	// Get-by-id (operator path) has no tenant in scope. Run under a
	// bypass-RLS tx so the FORCE-RLS policy does not hide the row.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: get begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return nil, fmt.Errorf("notifications: get bypass: %w", err)
	}
	rows, err := tx.Query(ctx, selectColumns+` from suite_notifications where id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("notifications: select: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	return scanRow(rows)
}

// List returns one page + total. Default sort is created_at desc so
// the dashboard renders "newest first" without client work.
func (r *Recorder) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if !r.HasPool() {
		// Tolerant: empty page so GET /api/v1/notifications renders.
		return &ListResult{Notifications: []Notification{}}, nil
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
	if f.Kind != "" {
		args = append(args, f.Kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}

	// When a tenant filter is present, bind it so RLS scopes the read to
	// that tenant. When it is absent this is a cross-tenant operator
	// listing with no single tenant to bind, so run under a bypass-RLS tx
	// (read-only; the defer'd rollback closes it).
	var q interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
		QueryRow(context.Context, string, ...any) pgx.Row
	} = r.pool
	if f.TenantID != "" {
		ctx = tenantctx.WithTenant(ctx, f.TenantID, "")
	} else {
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("notifications: list begin: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
			return nil, fmt.Errorf("notifications: list bypass: %w", err)
		}
		q = tx
	}

	var total int
	if err := q.QueryRow(ctx,
		"select count(*) from suite_notifications"+where,
		args...,
	).Scan(&total); err != nil {
		return nil, fmt.Errorf("notifications: count: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	query := selectColumns + " from suite_notifications" + where +
		fmt.Sprintf(" order by created_at desc limit $%d offset $%d", len(args)-1, len(args))

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("notifications: list: %w", err)
	}
	defer rows.Close()

	out := make([]Notification, 0, f.Limit)
	for rows.Next() {
		n, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ListResult{
		Notifications: out,
		Total:         total,
		HasMore:       f.Offset+len(out) < total,
	}, nil
}

// Stats returns the per-status, per-adapter, and today counters used
// by GET /api/v1/notifications/stats.
func (r *Recorder) Stats(ctx context.Context, tenantID string) (*Stats, error) {
	stats := &Stats{
		ByStatus:  map[Status]int{},
		ByAdapter: []AdapterCount{},
	}
	if !r.HasPool() {
		return stats, nil
	}
	// Bind the requested tenant so RLS on suite_notifications passes.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	// Build a parameterised tenant predicate that composes cleanly with
	// the time-window predicate below. tenantClause is "" or " and
	// tenant_id = $1", whichever the caller asked for.
	args := []any{}
	tenantClause := ""
	if tenantID != "" {
		args = append(args, tenantID)
		tenantClause = " and tenant_id = $1"
	}

	// Per-status counts (last 24h gives the "active range" the dashboard
	// expects — anything older is historical noise for the KPI strip).
	statusQ := `
		select status, count(*)::int
		  from suite_notifications
		 where created_at >= now() - interval '24 hours'` + tenantClause + `
		 group by status`
	statusRows, err := r.pool.Query(ctx, statusQ, args...)
	if err != nil {
		return nil, fmt.Errorf("notifications: stats by_status: %w", err)
	}
	for statusRows.Next() {
		var s string
		var c int
		if err := statusRows.Scan(&s, &c); err != nil {
			statusRows.Close()
			return nil, err
		}
		stats.ByStatus[Status(s)] = c
	}
	statusRows.Close()
	if err := statusRows.Err(); err != nil {
		return nil, err
	}

	// Per-adapter counts (24h window).
	adapterQ := `
		select coalesce(adapter, 'unassigned') as adapter, count(*)::int
		  from suite_notifications
		 where created_at >= now() - interval '24 hours'` + tenantClause + `
		 group by adapter
		 order by count(*) desc`
	adapterRows, err := r.pool.Query(ctx, adapterQ, args...)
	if err != nil {
		return nil, fmt.Errorf("notifications: stats by_adapter: %w", err)
	}
	for adapterRows.Next() {
		var ac AdapterCount
		if err := adapterRows.Scan(&ac.Adapter, &ac.Count); err != nil {
			adapterRows.Close()
			return nil, err
		}
		stats.ByAdapter = append(stats.ByAdapter, ac)
	}
	adapterRows.Close()
	if err := adapterRows.Err(); err != nil {
		return nil, err
	}

	// Today's sent/failed (UTC midnight boundary; matches the sandbox
	// pool stats convention so dashboards align). Tenant filter (when
	// set) lands at $2 because $1 is the date boundary.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayArgs := []any{today}
	todayTenantClause := ""
	if tenantID != "" {
		todayArgs = append(todayArgs, tenantID)
		todayTenantClause = " and tenant_id = $2"
	}
	if err := r.pool.QueryRow(ctx,
		`select
		  coalesce(sum(case when status = 'sent'   then 1 else 0 end), 0)::int,
		  coalesce(sum(case when status = 'failed' then 1 else 0 end), 0)::int
		 from suite_notifications
		 where created_at >= $1`+todayTenantClause,
		todayArgs...,
	).Scan(&stats.SentToday, &stats.FailedToday); err != nil {
		return nil, fmt.Errorf("notifications: stats today: %w", err)
	}

	return stats, nil
}

// ─── internals ────────────────────────────────────────────────────────────

const selectColumns = `
	select id, tenant_id, kind, adapter, template, "to", "from",
	       subject, data, status, provider_message_id, attempts,
	       last_error, scheduled_at, sent_at, created_at
`

// scanRow consumes one pgx.Rows position into a *Notification. Pointer
// columns map directly to the *T fields so NULL stays NULL.
func scanRow(rows pgx.Rows) (*Notification, error) {
	var (
		id                string
		tenantID          *string
		kind              string
		adapter           *string
		template          string
		to                string
		from              *string
		subject           *string
		dataJSON          []byte
		status            string
		providerMessageID *string
		attempts          int
		lastError         *string
		scheduledAt       time.Time
		sentAt            *time.Time
		createdAt         time.Time
	)
	if err := rows.Scan(
		&id, &tenantID, &kind, &adapter, &template, &to, &from,
		&subject, &dataJSON, &status, &providerMessageID, &attempts,
		&lastError, &scheduledAt, &sentAt, &createdAt,
	); err != nil {
		return nil, fmt.Errorf("notifications: scan: %w", err)
	}
	data := map[string]any{}
	if len(dataJSON) > 0 {
		if err := json.Unmarshal(dataJSON, &data); err != nil {
			return nil, fmt.Errorf("notifications: decode data: %w", err)
		}
	}
	n := &Notification{
		ID:                id,
		TenantID:          tenantID,
		Kind:              Kind(kind),
		Adapter:           adapter,
		Template:          template,
		To:                to,
		From:              from,
		Subject:           subject,
		Data:              data,
		Status:            Status(status),
		ProviderMessageID: providerMessageID,
		Attempts:          attempts,
		LastError:         lastError,
		ScheduledAt:       scheduledAt.UTC().Format(time.RFC3339Nano),
		CreatedAt:         createdAt.UTC().Format(time.RFC3339Nano),
	}
	if sentAt != nil {
		s := sentAt.UTC().Format(time.RFC3339Nano)
		n.SentAt = &s
	}
	return n, nil
}

// cloneData returns a shallow copy of data so callers can't mutate the
// stored map after Insert returns.
func cloneData(data map[string]any) map[string]any {
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}

func decodeMap(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out
}

// Keep pgx import alive even if scan paths get refactored.
var _ = errors.Is
