// SPDX-License-Identifier: Apache-2.0

// store.go — Postgres persistence for suite_invitations.
//
// Tenant-scoped writes/reads bind app.tenant_id (via tenantctx) so the
// FORCE-RLS policy permits them. The accept path is the one exception: the
// invitee has no membership yet, so AcceptByToken looks the row up by its
// unique token_hash under app.bypass_rls — the token itself is the
// capability that authorizes the read.
package invitations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Store wraps the SQL surface for suite_invitations.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewStore constructs a Store. pool may be nil (no-DB mode); log defaults
// to slog.Default().
func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log}
}

// HasPool reports whether the Store can talk to a database.
func (s *Store) HasPool() bool { return s != nil && s.pool != nil }

// CreateInput is the argument to Create.
type CreateInput struct {
	TenantID  string
	Email     string
	Role      string
	InvitedBy string        // suite_users.id of the inviter ("" = unknown)
	TTL       time.Duration // 0 → DefaultTTL
}

const invColumns = `id::text, tenant_id::text, email, role, status,
	invited_by::text, accepted_by::text, created_at, expires_at,
	accepted_at, revoked_at`

// Create inserts a pending invitation and returns it alongside the one-time
// plaintext token (never persisted). The row stores only the token hash.
func (s *Store) Create(ctx context.Context, in CreateInput) (Invitation, string, error) {
	if !s.HasPool() {
		return Invitation{}, "", ErrNotFound
	}
	tenantID := strings.TrimSpace(in.TenantID)
	email := strings.TrimSpace(in.Email)
	if tenantID == "" || email == "" {
		return Invitation{}, "", fmt.Errorf("%w: tenant_id and email are required", ErrInvalidInput)
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	token, hash, err := GenerateToken()
	if err != nil {
		return Invitation{}, "", fmt.Errorf("invitations: token entropy: %w", err)
	}

	// Bind tenant so the FORCE-RLS WITH CHECK passes.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	var invitedByArg *string
	if strings.TrimSpace(in.InvitedBy) != "" {
		v := in.InvitedBy
		invitedByArg = &v
	}
	expiresAt := time.Now().Add(ttl).UTC()

	row := s.pool.QueryRow(ctx, `
		insert into suite_invitations
			(tenant_id, email, role, token_hash, invited_by, expires_at)
		values ($1, $2, $3, $4, $5, $6)
		returning `+invColumns,
		tenantID, email, in.Role, hash, invitedByArg, expiresAt)
	inv, err := scanInvitation(row)
	if err != nil {
		return Invitation{}, "", fmt.Errorf("invitations: create: %w", err)
	}
	return inv, token, nil
}

// List returns the tenant's invitations, newest first.
func (s *Store) List(ctx context.Context, tenantID string) ([]Invitation, error) {
	if !s.HasPool() {
		return []Invitation{}, nil
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("%w: tenant_id required", ErrInvalidInput)
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	rows, err := s.pool.Query(ctx, `
		select `+invColumns+`
		  from suite_invitations
		 where tenant_id = $1
		 order by created_at desc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invitations: list: %w", err)
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// Revoke marks a pending invitation revoked. Tenant-scoped: the id must
// belong to tenantID (RLS enforces it too). Returns ErrNotFound when no
// pending row matched, ErrNotPending when it exists but isn't pending.
func (s *Store) Revoke(ctx context.Context, tenantID, id string) error {
	if !s.HasPool() {
		return ErrNotFound
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	// Load first so we can distinguish not-found from not-pending with a
	// precise error (the state machine owns the decision).
	row := s.pool.QueryRow(ctx, `select `+invColumns+` from suite_invitations where id = $1`, id)
	inv, err := scanInvitation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("invitations: revoke load: %w", err)
	}
	if err := CanRevoke(inv, time.Now()); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		update suite_invitations
		   set status = 'revoked', revoked_at = now()
		 where id = $1 and status = 'pending'
	`, id)
	if err != nil {
		return fmt.Errorf("invitations: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotPending
	}
	return nil
}

// AcceptByToken redeems a plaintext token for a membership grant. It looks
// the row up by token hash under bypass_rls (the invitee has no tenant
// binding), validates the state machine + email match, then flips the row
// to accepted. Returns the accepted invitation (tenant_id + role) so the
// caller can create the membership. The membership write is the caller's
// responsibility (kept out of this store to avoid a tenancy import cycle).
func (s *Store) AcceptByToken(ctx context.Context, token, principalUserID, principalEmail string) (Invitation, error) {
	if !s.HasPool() {
		return Invitation{}, ErrNotFound
	}
	if strings.TrimSpace(token) == "" {
		return Invitation{}, fmt.Errorf("%w: token required", ErrInvalidInput)
	}
	hash := HashToken(token)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("invitations: accept begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return Invitation{}, fmt.Errorf("invitations: accept bypass: %w", err)
	}

	row := tx.QueryRow(ctx, `select `+invColumns+` from suite_invitations where token_hash = $1`, hash)
	inv, err := scanInvitation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, ErrNotFound
		}
		return Invitation{}, fmt.Errorf("invitations: accept load: %w", err)
	}
	now := time.Now()
	if err := CanAccept(inv, now); err != nil {
		return Invitation{}, err
	}
	if strings.TrimSpace(principalEmail) != "" && !EmailMatches(inv.Email, principalEmail) {
		return Invitation{}, ErrEmailMismatch
	}

	var acceptedByArg *string
	if strings.TrimSpace(principalUserID) != "" {
		v := principalUserID
		acceptedByArg = &v
	}
	row = tx.QueryRow(ctx, `
		update suite_invitations
		   set status = 'accepted', accepted_at = now(), accepted_by = $2
		 where id = $1 and status = 'pending'
		returning `+invColumns,
		inv.ID, acceptedByArg)
	accepted, err := scanInvitation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost a race — another accept flipped it first.
			return Invitation{}, ErrNotPending
		}
		return Invitation{}, fmt.Errorf("invitations: accept update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("invitations: accept commit: %w", err)
	}
	return accepted, nil
}

// ExpireStale bulk-flips pending invitations past their expires_at to the
// 'expired' status. Runs cross-tenant under bypass_rls; intended for a
// platform cron. Returns the number of rows expired.
func (s *Store) ExpireStale(ctx context.Context) (int64, error) {
	if !s.HasPool() {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("invitations: expire begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return 0, fmt.Errorf("invitations: expire bypass: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		update suite_invitations
		   set status = 'expired'
		 where status = 'pending' and expires_at <= now()
	`)
	if err != nil {
		return 0, fmt.Errorf("invitations: expire: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("invitations: expire commit: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanInvitation(row pgx.Row) (Invitation, error) {
	var (
		inv        Invitation
		status     string
		invitedBy  *string
		acceptedBy *string
	)
	if err := row.Scan(
		&inv.ID, &inv.TenantID, &inv.Email, &inv.Role, &status,
		&invitedBy, &acceptedBy, &inv.CreatedAt, &inv.ExpiresAt,
		&inv.AcceptedAt, &inv.RevokedAt,
	); err != nil {
		return Invitation{}, err
	}
	inv.Status = Status(status)
	inv.InvitedBy = invitedBy
	inv.AcceptedBy = acceptedBy
	inv.CreatedAt = inv.CreatedAt.UTC()
	inv.ExpiresAt = inv.ExpiresAt.UTC()
	return inv, nil
}
