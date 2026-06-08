// SPDX-License-Identifier: Apache-2.0

package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type dbPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool dbPool
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("approvals: pool required")
	}
	return NewWithPool(pool), nil
}

func NewWithPool(pool dbPool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Request(ctx context.Context, in RequestInput) (Approval, error) {
	if s == nil || s.pool == nil {
		return Approval{}, errors.New("approvals: store not configured")
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Approval{}, ErrTenantRequired
	}
	kind := strings.TrimSpace(in.Kind)
	if err := validateKind(kind); err != nil {
		return Approval{}, err
	}
	payload, err := encodePayload(in.Payload)
	if err != nil {
		return Approval{}, err
	}
	requestedBy := strings.TrimSpace(in.RequestedBy)
	if requestedBy == "" {
		requestedBy = tenantctx.UserID(ctx)
	}
	row := s.pool.QueryRow(ctx, `
		insert into suite_approvals
			(tenant_id, requested_by, kind, payload, status, created_at, updated_at)
		values
			($1, nullif($2, '')::uuid, $3, $4, $5, now(), now())
		returning id::text, tenant_id::text, requested_by::text, kind, payload,
		          status, decided_by::text, decided_at, decision_note,
		          created_at, updated_at
	`, tenantID, requestedBy, kind, payload, StatusPending)
	return scanApproval(row)
}

func (s *Store) Get(ctx context.Context, id string) (Approval, error) {
	if s == nil || s.pool == nil {
		return Approval{}, errors.New("approvals: store not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Approval{}, fmt.Errorf("%w: approval id is required", ErrValidation)
	}
	return scanApproval(s.pool.QueryRow(ctx, `
		select id::text, tenant_id::text, requested_by::text, kind, payload,
		       status, decided_by::text, decided_at, decision_note,
		       created_at, updated_at
		from suite_approvals
		where id = $1
	`, id))
}

func (s *Store) List(ctx context.Context, in ListInput) (ListResult, error) {
	if s == nil || s.pool == nil {
		return ListResult{}, errors.New("approvals: store not configured")
	}
	if tenantctx.TenantID(ctx) == "" {
		return ListResult{Approvals: []Approval{}}, ErrTenantRequired
	}
	status := strings.TrimSpace(in.Status)
	if status != "" && !validStatus(status) {
		return ListResult{Approvals: []Approval{}}, fmt.Errorf("%w: invalid status", ErrValidation)
	}
	args := []any{clamp(in.Limit, 1, 100, 25), max(in.Offset, 0)}
	conds := []string{"true"}
	if status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if kind := strings.TrimSpace(in.Kind); kind != "" {
		args = append(args, kind)
		conds = append(conds, fmt.Sprintf("kind = $%d", len(args)))
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		with filtered as (
			select id::text, tenant_id::text, requested_by::text, kind, payload,
			       status, decided_by::text, decided_at, decision_note,
			       created_at, updated_at, count(*) over() as total
			from suite_approvals
			where %s
			order by created_at desc
			limit $1 offset $2
		)
		select * from filtered
	`, strings.Join(conds, " and ")), args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("approvals: list: %w", err)
	}
	defer rows.Close()
	out := ListResult{Approvals: []Approval{}}
	for rows.Next() {
		a, total, err := scanApprovalWithTotal(rows)
		if err != nil {
			return ListResult{}, err
		}
		out.Approvals = append(out.Approvals, a)
		out.Total = total
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("approvals: list rows: %w", err)
	}
	out.HasMore = max(in.Offset, 0)+len(out.Approvals) < out.Total
	return out, nil
}

func (s *Store) Decide(ctx context.Context, in DecideInput) (Approval, error) {
	if s == nil || s.pool == nil {
		return Approval{}, errors.New("approvals: store not configured")
	}
	id := strings.TrimSpace(in.ID)
	status := strings.TrimSpace(in.Status)
	if id == "" {
		return Approval{}, fmt.Errorf("%w: approval id is required", ErrValidation)
	}
	if status != StatusApproved && status != StatusDenied && status != StatusCancelled {
		return Approval{}, fmt.Errorf("%w: decision status must be approved, denied, or cancelled", ErrValidation)
	}
	decidedBy := strings.TrimSpace(in.DecidedBy)
	if decidedBy == "" {
		decidedBy = tenantctx.UserID(ctx)
	}
	row := s.pool.QueryRow(ctx, `
		update suite_approvals
		set status = $2,
		    decided_by = nullif($3, '')::uuid,
		    decided_at = now(),
		    decision_note = nullif($4, ''),
		    updated_at = now()
		where id = $1 and status = 'pending'
		returning id::text, tenant_id::text, requested_by::text, kind, payload,
		          status, decided_by::text, decided_at, decision_note,
		          created_at, updated_at
	`, id, status, decidedBy, strings.TrimSpace(in.DecisionNote))
	return scanApproval(row)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanApproval(row rowScanner) (Approval, error) {
	a, err := scanApprovalBase(row)
	if err != nil {
		return Approval{}, err
	}
	return a, nil
}

func scanApprovalWithTotal(row rowScanner) (Approval, int, error) {
	var total int
	a, err := scanApprovalBase(row, &total)
	return a, total, err
}

func scanApprovalBase(row rowScanner, extra ...any) (Approval, error) {
	var a Approval
	var payloadRaw []byte
	args := []any{
		&a.ID, &a.TenantID, &a.RequestedBy, &a.Kind, &payloadRaw,
		&a.Status, &a.DecidedBy, &a.DecidedAt, &a.DecisionNote,
		&a.CreatedAt, &a.UpdatedAt,
	}
	args = append(args, extra...)
	if err := row.Scan(args...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Approval{}, ErrNotFound
		}
		return Approval{}, fmt.Errorf("approvals: scan: %w", err)
	}
	a.Payload = map[string]any{}
	if len(payloadRaw) > 0 {
		_ = json.Unmarshal(payloadRaw, &a.Payload)
	}
	if a.Payload == nil {
		a.Payload = map[string]any{}
	}
	return a, nil
}

func validateKind(kind string) error {
	if kind == "" || len(kind) > 200 {
		return fmt.Errorf("%w: kind must be 1-200 characters", ErrValidation)
	}
	return nil
}

func encodePayload(payload map[string]any) ([]byte, error) {
	if payload == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(payload)
	if err != nil || string(b) == "null" {
		return []byte(`{}`), err
	}
	return b, nil
}

func validStatus(status string) bool {
	switch status {
	case StatusPending, StatusApproved, StatusDenied, StatusCancelled:
		return true
	default:
		return false
	}
}

func clamp(v, min, maxVal, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < min {
		return min
	}
	if v > maxVal {
		return maxVal
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
