// SPDX-License-Identifier: Apache-2.0

// Package idempotency implements runtime-wide inbound idempotency with
// response replay (PRD R1).
//
// The gateway middleware (services/runtime/internal/server) consults a
// Store for every mutating /api/v1 request that carries an
// Idempotency-Key header. The Store records, per (tenant_id, key), the
// request fingerprint and — once the handler completes — the captured
// response, so a retry with the same key replays the stored response
// without re-running the handler.
//
// The Store interface is the small exported helper other subsystems
// (webhooks, billing) can consult directly to make their own inbound
// operations idempotent — they call Reserve / Save / Release with the
// tenant-bound context exactly as the middleware does.
//
// Tenant scoping: every method derives the tenant from the request
// context (tenantctx.TenantID). The backing table is tenant-owned with
// FORCE ROW LEVEL SECURITY, so the tenant boundary is enforced at the
// database regardless of what a caller passes.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// ErrTenantRequired is returned when a Store method is called without a
// resolved tenant on the context. Callers (the middleware) treat this as
// "cannot make this request idempotent" and fall through to a normal,
// non-idempotent execution.
var ErrTenantRequired = errors.New("idempotency: tenant context required")

// Outcome is the result of reserving an idempotency key.
type Outcome int

const (
	// OutcomeAcquired means this caller now owns the key: no prior row
	// existed, so the handler should run and its response saved.
	OutcomeAcquired Outcome = iota
	// OutcomeReplay means a completed row with the SAME fingerprint
	// exists; the caller must replay Reservation.Response and NOT run
	// the handler.
	OutcomeReplay
	// OutcomeMismatch means a row exists for this key with a DIFFERENT
	// fingerprint; the caller must return 422 IDEMPOTENCY_KEY_REUSED.
	OutcomeMismatch
	// OutcomeInFlight means a row exists for this key that is still
	// reserved (no response yet); the caller must return 409
	// IDEMPOTENCY_IN_FLIGHT.
	OutcomeInFlight
)

// StoredResponse is the captured response replayed on a duplicate request.
// Body holds the raw bytes so replays are byte-identical.
type StoredResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// Reservation is the result of Store.Reserve. Response is non-nil only
// when Outcome == OutcomeReplay.
type Reservation struct {
	Outcome  Outcome
	Response *StoredResponse
}

// Store is the idempotency persistence contract. The middleware depends on
// this interface (so tests inject a fake); PgStore is the production
// implementation. Every method scopes to the tenant on ctx.
type Store interface {
	// Reserve atomically claims (tenant, key) for fingerprint, or reports
	// what already exists (replay / mismatch / in-flight).
	Reserve(ctx context.Context, key, fingerprint string) (Reservation, error)
	// Save records the final response for a key this caller reserved,
	// marking it completed and replayable.
	Save(ctx context.Context, key string, resp StoredResponse) error
	// Release removes a still-reserved (not yet completed) row so a retry
	// can proceed. A no-op on an already-completed row.
	Release(ctx context.Context, key string) error
}

// Fingerprint canonicalises a request into a stable hash. Two requests
// with the same method, path, query string and body produce the same
// fingerprint; any difference (a reused key on a different operation)
// produces a different one, which the middleware turns into a 422.
func Fingerprint(method, path, rawQuery string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	h.Write([]byte(rawQuery))
	h.Write([]byte{'\n'})
	h.Write(bodyHash[:])
	return hex.EncodeToString(h.Sum(nil))
}

// dbPool is the subset of pgxpool used by PgStore (mirrors the approvals
// store) so unit tests can supply a fake without a live database.
type dbPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PgStore is the Postgres-backed Store. It relies on the pool's
// PrepareConn hook (internal/db) binding app.tenant_id from the context,
// so all reads/writes are RLS-scoped to the caller's tenant.
type PgStore struct {
	pool dbPool
}

// New constructs a PgStore over the given pool.
func New(pool *pgxpool.Pool) (*PgStore, error) {
	if pool == nil {
		return nil, errors.New("idempotency: pool required")
	}
	return &PgStore{pool: pool}, nil
}

// NewWithPool wraps any dbPool (production pool or a test fake).
func NewWithPool(pool dbPool) *PgStore {
	return &PgStore{pool: pool}
}

// Reserve implements Store.
func (s *PgStore) Reserve(ctx context.Context, key, fingerprint string) (Reservation, error) {
	if s == nil || s.pool == nil {
		return Reservation{}, errors.New("idempotency: store not configured")
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Reservation{}, ErrTenantRequired
	}

	// Opportunistic purge of this tenant's expired rows (RLS-scoped by the
	// bound app.tenant_id). Keeps the table bounded and lets a key be
	// reused after its 24h TTL. Best-effort — never fail the reserve on it.
	_, _ = s.pool.Exec(ctx, `delete from suite_idempotency_keys where expires_at < now()`)

	// Atomically claim the key. ON CONFLICT DO NOTHING + RETURNING means we
	// won the race iff a row comes back.
	var id string
	err := s.pool.QueryRow(ctx, `
		insert into suite_idempotency_keys (tenant_id, idempotency_key, fingerprint)
		values ($1, $2, $3)
		on conflict (tenant_id, idempotency_key) do nothing
		returning id::text
	`, tenantID, key, fingerprint).Scan(&id)
	if err == nil {
		return Reservation{Outcome: OutcomeAcquired}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, fmt.Errorf("idempotency: reserve insert: %w", err)
	}

	// A row already exists — read it to decide replay / mismatch / in-flight.
	var (
		existingFingerprint string
		statusCode          *int
		headersRaw          []byte
		body                []byte
	)
	err = s.pool.QueryRow(ctx, `
		select fingerprint, status_code, response_headers, response_body
		from suite_idempotency_keys
		where tenant_id = $1 and idempotency_key = $2
	`, tenantID, key).Scan(&existingFingerprint, &statusCode, &headersRaw, &body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row vanished between the insert-conflict and this read
			// (a concurrent owner completed and the purge removed it). Treat
			// as in-flight; the client should retry.
			return Reservation{Outcome: OutcomeInFlight}, nil
		}
		return Reservation{}, fmt.Errorf("idempotency: reserve read: %w", err)
	}
	if existingFingerprint != fingerprint {
		return Reservation{Outcome: OutcomeMismatch}, nil
	}
	if statusCode == nil {
		return Reservation{Outcome: OutcomeInFlight}, nil
	}
	return Reservation{
		Outcome: OutcomeReplay,
		Response: &StoredResponse{
			Status:  *statusCode,
			Headers: decodeHeaders(headersRaw),
			Body:    body,
		},
	}, nil
}

// Save implements Store.
func (s *PgStore) Save(ctx context.Context, key string, resp StoredResponse) error {
	if s == nil || s.pool == nil {
		return errors.New("idempotency: store not configured")
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return ErrTenantRequired
	}
	headers, err := json.Marshal(resp.Headers)
	if err != nil {
		headers = []byte("{}")
	}
	_, err = s.pool.Exec(ctx, `
		update suite_idempotency_keys
		set status_code = $3, response_headers = $4, response_body = $5, completed_at = now()
		where tenant_id = $1 and idempotency_key = $2
	`, tenantID, key, resp.Status, headers, resp.Body)
	if err != nil {
		return fmt.Errorf("idempotency: save: %w", err)
	}
	return nil
}

// Release implements Store.
func (s *PgStore) Release(ctx context.Context, key string) error {
	if s == nil || s.pool == nil {
		return errors.New("idempotency: store not configured")
	}
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return ErrTenantRequired
	}
	// Only release a still-reserved row; never delete a completed one (its
	// stored response must survive for future replays).
	_, err := s.pool.Exec(ctx, `
		delete from suite_idempotency_keys
		where tenant_id = $1 and idempotency_key = $2 and status_code is null
	`, tenantID, key)
	if err != nil {
		return fmt.Errorf("idempotency: release: %w", err)
	}
	return nil
}

// PurgeExpired deletes expired rows visible under the current RLS context.
// Called with a tenant-bound context it purges that tenant; called with a
// bypass_rls context (an operator/cron sweep) it purges all tenants.
// Returns the number of rows removed.
func (s *PgStore) PurgeExpired(ctx context.Context) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("idempotency: store not configured")
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_idempotency_keys where expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("idempotency: purge: %w", err)
	}
	return tag.RowsAffected(), nil
}

func decodeHeaders(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}
