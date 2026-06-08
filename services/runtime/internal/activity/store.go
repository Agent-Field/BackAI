// SPDX-License-Identifier: Apache-2.0

package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const tracerName = "af-stack/activity"

type Store struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	tracer trace.Tracer
}

func New(pool *pgxpool.Pool, log *slog.Logger) (*Store, error) {
	if pool == nil {
		return nil, errors.New("activity: pool required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log, tracer: otel.Tracer(tracerName)}, nil
}

func (s *Store) Log(ctx context.Context, in LogInput) (Entry, error) {
	ctx, span := s.tracer.Start(ctx, "activity.log", trace.WithAttributes(
		attribute.String("activity.action", in.Action),
		attribute.String("activity.resource_type", in.ResourceType),
	))
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Entry{}, ErrTenantRequired
	}
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		userID = tenantctx.UserID(ctx)
	}
	apiKeyID := tenantctx.APIKeyID(ctx)
	actorType, err := resolveActorType(in.ActorType, userID, apiKeyID)
	if err != nil {
		return Entry{}, err
	}
	action := strings.TrimSpace(in.Action)
	if err := validateAction(action); err != nil {
		return Entry{}, err
	}
	resourceType := nullableText(in.ResourceType)
	resourceID := nullableText(in.ResourceID)
	if resourceType == nil && resourceID != nil {
		return Entry{}, fmt.Errorf("%w: resource_type is required when resource_id is set", ErrValidation)
	}
	metadata, err := encodeMetadata(in.Metadata)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: metadata must be JSON-serializable", ErrValidation)
	}
	occurredAt := time.Now().UTC()
	if in.OccurredAt != nil {
		occurredAt = in.OccurredAt.UTC()
	}

	row := s.pool.QueryRow(ctx, `
		insert into suite_user_activity
			(tenant_id, user_id, api_key_id, actor_type, action, resource_type,
			 resource_id, metadata, ip, user_agent, occurred_at)
		values
			($1, nullif($2, '')::uuid, nullif($3, '')::uuid, $4, $5, $6,
			 $7, $8, nullif($9, '')::inet, $10, $11)
		returning id::text, tenant_id::text, user_id::text, api_key_id::text,
		          actor_type, action, resource_type, resource_id, metadata,
		          ip::text, user_agent, occurred_at
	`, tenantID, userID, apiKeyID, string(actorType), action, resourceType,
		resourceID, metadata, truncate(in.IP, 128), nullableText(in.UserAgent), occurredAt)

	entry, err := scanEntry(row)
	if err != nil {
		span.RecordError(err)
		return Entry{}, fmt.Errorf("activity: log: %w", err)
	}
	return entry, nil
}

func (s *Store) List(ctx context.Context, f ListFilter) (Page, error) {
	ctx, span := s.tracer.Start(ctx, "activity.list", trace.WithAttributes(
		attribute.String("activity.action", f.Action),
		attribute.String("activity.resource_type", f.ResourceType),
		attribute.Int("activity.limit", f.Limit),
	))
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Page{Entries: []Entry{}}, ErrTenantRequired
	}
	conds := []string{"tenant_id = $1"}
	args := []any{tenantID}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.UserID != "" {
		conds = append(conds, "user_id::text = "+bind(f.UserID))
	}
	if f.Action != "" {
		conds = append(conds, "action = "+bind(f.Action))
	}
	if f.ResourceType != "" {
		conds = append(conds, "resource_type = "+bind(f.ResourceType))
	}
	if f.ResourceID != "" {
		conds = append(conds, "resource_id = "+bind(f.ResourceID))
	}
	if f.From != nil {
		conds = append(conds, "occurred_at >= "+bind(f.From.UTC()))
	}
	if f.To != nil {
		conds = append(conds, "occurred_at <= "+bind(f.To.UTC()))
	}
	where := " where " + strings.Join(conds, " and ")
	limit := clampLimit(f.Limit)
	offset := clampOffset(f.Offset)

	var total int
	if err := s.pool.QueryRow(ctx, "select count(*) from suite_user_activity"+where, args...).Scan(&total); err != nil {
		span.RecordError(err)
		return Page{Entries: []Entry{}}, fmt.Errorf("activity: count: %w", err)
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	rowsSQL := `select id::text, tenant_id::text, user_id::text, api_key_id::text,
		              actor_type, action, resource_type, resource_id, metadata,
		              ip::text, user_agent, occurred_at
		         from suite_user_activity` + where +
		fmt.Sprintf(" order by occurred_at desc limit $%d offset $%d", len(pageArgs)-1, len(pageArgs))
	rows, err := s.pool.Query(ctx, rowsSQL, pageArgs...)
	if err != nil {
		span.RecordError(err)
		return Page{Entries: []Entry{}}, fmt.Errorf("activity: list: %w", err)
	}
	defer rows.Close()

	out := Page{Entries: []Entry{}, Total: total}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			span.RecordError(scanErr)
			return Page{Entries: []Entry{}}, fmt.Errorf("activity: scan: %w", scanErr)
		}
		out.Entries = append(out.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{Entries: []Entry{}}, fmt.Errorf("activity: iter: %w", err)
	}
	out.HasMore = offset+len(out.Entries) < total
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(r rowScanner) (Entry, error) {
	var (
		e          Entry
		userID     *string
		apiKeyID   *string
		resourceTy *string
		resourceID *string
		metaRaw    []byte
		ip         *string
		userAgent  *string
		occurredAt time.Time
		actorType  string
	)
	if err := r.Scan(&e.ID, &e.TenantID, &userID, &apiKeyID, &actorType, &e.Action,
		&resourceTy, &resourceID, &metaRaw, &ip, &userAgent, &occurredAt); err != nil {
		return Entry{}, err
	}
	e.UserID = userID
	e.APIKeyID = apiKeyID
	e.ActorType = ActorType(actorType)
	e.ResourceType = resourceTy
	e.ResourceID = resourceID
	e.IP = ip
	e.UserAgent = userAgent
	e.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
	e.Metadata = map[string]any{}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &e.Metadata)
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	return e, nil
}

func resolveActorType(in ActorType, userID, apiKeyID string) (ActorType, error) {
	if !in.Valid() {
		return "", fmt.Errorf("%w: actor_type must be user, api_key, system, or anonymous", ErrValidation)
	}
	if in != "" {
		return in, nil
	}
	switch {
	case userID != "":
		return ActorUser, nil
	case apiKeyID != "":
		return ActorAPIKey, nil
	default:
		return ActorSystem, nil
	}
}

func validateAction(action string) error {
	if action == "" || len(action) > 200 {
		return fmt.Errorf("%w: action must be 1-200 characters", ErrValidation)
	}
	return nil
}

func encodeMetadata(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte(`{}`), nil
	}
	return b, nil
}

func nullableText(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func truncate(v string, max int) string {
	v = strings.TrimSpace(v)
	if len(v) <= max {
		return v
	}
	return v[:max]
}
