// SPDX-License-Identifier: Apache-2.0

// deliveries.go — persistence layer for suite_webhook_deliveries.
//
// CO-OWNED with Phase 10.2. Phase 10.2 owns the canonical version of
// this file (its inbound code calls Insert with direction='inbound' and
// reads dedup tokens through the same row). Phase 10.3 needs the same
// store for the outbound path — both phases write it identically and
// the merge is a no-op when the file already exists.
//
// The store is intentionally small: Insert, Get, List, UpdateStatus,
// ClaimQueued. Anything richer (e.g. body storage offload, paging
// cursors) goes in a dedicated helper.
package webhooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeliveryStore wraps a pgxpool for the suite_webhook_deliveries table.
//
// All methods take a context and respect cancellation. The store is
// safe for concurrent use from multiple goroutines.
type DeliveryStore struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewDeliveryStore builds a store around the given pool. Returns nil
// when pool is nil so callers can compose unconditionally and degrade
// to "store not configured" at the boundary.
func NewDeliveryStore(pool *pgxpool.Pool, log *slog.Logger) *DeliveryStore {
	if pool == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &DeliveryStore{pool: pool, log: log}
}

// HasPool reports whether the store is actually wired to a database.
// Used by callers that compose with a fallback (in-memory mode for tests).
func (s *DeliveryStore) HasPool() bool {
	return s != nil && s.pool != nil
}

// InsertParams is the shape Insert expects. Headers + Body live in
// columns; preview is auto-derived from Body when the caller doesn't
// supply one.
//
// DedupToken (optional) participates in the partial-unique index
// (endpoint_id, dedup_token). When the caller supplies a token AND a
// row already exists for that (endpoint_id, token) pair, Insert
// returns ErrDuplicateDelivery — the inbound handler maps that to a
// 'dropped_duplicate' status (without the token, so the audit row
// inserts cleanly).
type InsertParams struct {
	EndpointID  *string
	TenantID    *string
	Direction   Direction
	Destination string
	EventType   string
	Status      Status
	Headers     map[string]string
	Body        []byte
	BodyPreview *string
	DedupToken  *string
	Attempts    int
	ScheduledAt time.Time
}

// Insert creates a new delivery row and returns the rendered Delivery.
// Caller is responsible for passing the correct direction.
func (s *DeliveryStore) Insert(ctx context.Context, p InsertParams) (*Delivery, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if p.Direction == "" {
		p.Direction = DirectionOutbound
	}
	if p.Status == "" {
		p.Status = StatusQueued
	}
	if p.ScheduledAt.IsZero() {
		p.ScheduledAt = time.Now().UTC()
	}
	if p.BodyPreview == nil && len(p.Body) > 0 {
		prev := truncatePreview(p.Body, BodyPreviewBytes)
		p.BodyPreview = &prev
	}

	headersJSON, err := marshalHeaders(p.Headers)
	if err != nil {
		return nil, fmt.Errorf("webhooks: marshal headers: %w", err)
	}

	const q = `
        insert into suite_webhook_deliveries
            (endpoint_id, tenant_id, direction, destination, event_type,
             status, attempts, headers, body, body_preview,
             dedup_token, scheduled_at, created_at)
        values
            ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, now())
        returning id, created_at::text, scheduled_at::text
    `
	var (
		id          string
		createdAt   string
		scheduledAt string
	)
	err = s.pool.QueryRow(ctx, q,
		p.EndpointID,
		p.TenantID,
		string(p.Direction),
		p.Destination,
		p.EventType,
		string(p.Status),
		p.Attempts,
		headersJSON,
		p.Body,
		p.BodyPreview,
		p.DedupToken,
		p.ScheduledAt,
	).Scan(&id, &createdAt, &scheduledAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateDelivery
		}
		return nil, fmt.Errorf("webhooks: insert delivery: %w", err)
	}

	d := &Delivery{
		ID:          id,
		EndpointID:  p.EndpointID,
		TenantID:    p.TenantID,
		Direction:   p.Direction,
		Destination: p.Destination,
		EventType:   p.EventType,
		Status:      p.Status,
		Attempts:    p.Attempts,
		BodyPreview: p.BodyPreview,
		ScheduledAt: scheduledAt,
		CreatedAt:   createdAt,
		Headers:     p.Headers,
		Body:        append([]byte(nil), p.Body...),
	}
	return d, nil
}

// Get returns the row with the given id; nil + ErrNotFound when missing.
// Body + Headers are populated so the caller (the worker) can re-POST.
func (s *DeliveryStore) Get(ctx context.Context, id string) (*Delivery, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	const q = `
        select
            id,
            endpoint_id,
            tenant_id,
            direction,
            destination,
            event_type,
            status,
            attempts,
            response_status,
            response_ms,
            body,
            body_preview,
            body_storage_url,
            last_error,
            headers,
            scheduled_at::text,
            delivered_at::text,
            created_at::text
        from suite_webhook_deliveries
        where id = $1
    `
	row := s.pool.QueryRow(ctx, q, id)
	d, err := scanDelivery(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("webhooks: get delivery: %w", err)
	}
	return d, nil
}

// List returns a page of deliveries matching the filters, in DESC order
// of created_at. Total is the unfiltered row count for the filter
// predicate (without LIMIT) so the dashboard can paginate.
func (s *DeliveryStore) List(ctx context.Context, f ListFilters) (*ListResult, error) {
	if !s.HasPool() {
		return &ListResult{Deliveries: []Delivery{}}, nil
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var (
		where []string
		args  []any
	)
	add := func(predicate string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(predicate, len(args)))
	}
	if f.Direction != "" {
		add("direction = $%d", string(f.Direction))
	}
	if f.EndpointID != "" {
		add("endpoint_id = $%d", f.EndpointID)
	}
	if f.TenantID != "" {
		add("tenant_id = $%d", f.TenantID)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "where " + strings.Join(where, " and ")
	}

	countQ := "select count(*) from suite_webhook_deliveries " + whereClause
	var total int
	if err := s.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("webhooks: list count: %w", err)
	}

	listQ := fmt.Sprintf(`
        select
            id,
            endpoint_id,
            tenant_id,
            direction,
            destination,
            event_type,
            status,
            attempts,
            response_status,
            response_ms,
            body,
            body_preview,
            body_storage_url,
            last_error,
            headers,
            scheduled_at::text,
            delivered_at::text,
            created_at::text
        from suite_webhook_deliveries
        %s
        order by created_at desc
        limit $%d offset $%d
    `, whereClause, len(args)+1, len(args)+2)
	args = append(args, f.Limit, f.Offset)

	rows, err := s.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, fmt.Errorf("webhooks: list query: %w", err)
	}
	defer rows.Close()

	out := make([]Delivery, 0, f.Limit)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("webhooks: list scan: %w", err)
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: list rows: %w", err)
	}

	return &ListResult{
		Deliveries: out,
		Total:      total,
		HasMore:    f.Offset+len(out) < total,
	}, nil
}

// UpdateParams carries the columns UpdateStatus may write. nil pointer
// fields are left untouched on the row (UpdateStatus uses COALESCE).
type UpdateParams struct {
	Status         Status
	Attempts       *int
	ResponseStatus *int
	ResponseMS     *int
	LastError      *string
	ScheduledAt    *time.Time
	DeliveredAt    *time.Time
}

// UpdateStatus writes the new state to the row. Returns ErrNotFound
// when the row id doesn't exist.
func (s *DeliveryStore) UpdateStatus(ctx context.Context, id string, p UpdateParams) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	const q = `
        update suite_webhook_deliveries
        set
            status          = $2,
            attempts        = coalesce($3, attempts),
            response_status = coalesce($4, response_status),
            response_ms     = coalesce($5, response_ms),
            last_error      = coalesce($6, last_error),
            scheduled_at    = coalesce($7, scheduled_at),
            delivered_at    = coalesce($8, delivered_at)
        where id = $1
    `
	tag, err := s.pool.Exec(ctx, q,
		id,
		string(p.Status),
		p.Attempts,
		p.ResponseStatus,
		p.ResponseMS,
		p.LastError,
		p.ScheduledAt,
		p.DeliveredAt,
	)
	if err != nil {
		return fmt.Errorf("webhooks: update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetForRetry zeroes attempts + clears last_error and re-queues the
// row at status='queued' with scheduled_at=now(). Used by the
// `/retry` endpoint.
func (s *DeliveryStore) ResetForRetry(ctx context.Context, id string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	const q = `
        update suite_webhook_deliveries
        set status       = 'queued',
            attempts     = 0,
            last_error   = null,
            scheduled_at = now(),
            delivered_at = null
        where id = $1
    `
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("webhooks: reset for retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimQueued is the worker's batch query. It picks up to `limit`
// outbound rows that are due (scheduled_at <= now()) and below the
// retry cap, transitioning them to status='delivering' atomically with
// SELECT ... FOR UPDATE SKIP LOCKED so multiple workers can run in
// parallel without claiming the same row.
func (s *DeliveryStore) ClaimQueued(ctx context.Context, limit int) ([]Delivery, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if limit <= 0 {
		limit = BatchSize
	}

	const q = `
        with claimed as (
            select id
            from suite_webhook_deliveries
            where direction = 'outbound'
              and status in ('queued', 'failed')
              and attempts < $1
              and scheduled_at <= now()
            order by scheduled_at
            limit $2
            for update skip locked
        )
        update suite_webhook_deliveries d
        set status = 'delivering'
        from claimed
        where d.id = claimed.id
        returning
            d.id,
            d.endpoint_id,
            d.tenant_id,
            d.direction,
            d.destination,
            d.event_type,
            d.status,
            d.attempts,
            d.response_status,
            d.response_ms,
            d.body,
            d.body_preview,
            d.body_storage_url,
            d.last_error,
            d.headers,
            d.scheduled_at::text,
            d.delivered_at::text,
            d.created_at::text
    `
	rows, err := s.pool.Query(ctx, q, MaxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("webhooks: claim queued: %w", err)
	}
	defer rows.Close()

	out := make([]Delivery, 0, limit)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("webhooks: claim scan: %w", err)
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhooks: claim rows: %w", err)
	}
	return out, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────

// rowScanner abstracts pgx.Row + pgx.Rows so scanDelivery can serve both
// QueryRow (Get) and Query (List/Claim) callers.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDelivery(r rowScanner) (*Delivery, error) {
	var (
		d              Delivery
		endpointID     sql.NullString
		tenantID       sql.NullString
		responseStatus sql.NullInt32
		responseMS     sql.NullInt32
		body           []byte
		bodyPreview    sql.NullString
		bodyStorageURL sql.NullString
		lastError      sql.NullString
		headersJSON    []byte
		deliveredAt    sql.NullString
		directionStr   string
		statusStr      string
	)
	err := r.Scan(
		&d.ID,
		&endpointID,
		&tenantID,
		&directionStr,
		&d.Destination,
		&d.EventType,
		&statusStr,
		&d.Attempts,
		&responseStatus,
		&responseMS,
		&body,
		&bodyPreview,
		&bodyStorageURL,
		&lastError,
		&headersJSON,
		&d.ScheduledAt,
		&deliveredAt,
		&d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	d.Direction = Direction(directionStr)
	d.Status = Status(statusStr)
	if endpointID.Valid {
		v := endpointID.String
		d.EndpointID = &v
	}
	if tenantID.Valid {
		v := tenantID.String
		d.TenantID = &v
	}
	if responseStatus.Valid {
		v := int(responseStatus.Int32)
		d.ResponseStatus = &v
	}
	if responseMS.Valid {
		v := int(responseMS.Int32)
		d.ResponseMS = &v
	}
	if bodyPreview.Valid {
		v := bodyPreview.String
		d.BodyPreview = &v
	}
	if bodyStorageURL.Valid {
		v := bodyStorageURL.String
		d.BodyStorageURL = &v
	}
	if lastError.Valid {
		v := lastError.String
		d.LastError = &v
	}
	if deliveredAt.Valid {
		v := deliveredAt.String
		d.DeliveredAt = &v
	}
	if len(body) > 0 {
		d.Body = body
	}
	if len(headersJSON) > 0 {
		hdr, err := unmarshalHeaders(headersJSON)
		if err != nil {
			return nil, fmt.Errorf("webhooks: unmarshal headers: %w", err)
		}
		d.Headers = hdr
	}
	return &d, nil
}