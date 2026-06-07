// store.go — persistence layer for suite_mcp_servers.
//
// Methods are intentionally small and orthogonal:
//
//   List               — page-less read; the dashboard never has more
//                        than a handful of MCP servers per tenant.
//   Get                — single row lookup.
//   Upsert             — install/replace a row from CreateMCPServerInput.
//                        Idempotent so re-adding by name overwrites.
//   Delete             — drop a row; Pool tears down the adapter on
//                        the next Reconcile call.
//   SetEnabled         — operator toggle.
//   RecordStatus       — Pool writes lifecycle transitions here.
//   RecordToolsCount   — Pool stamps the cached count after every
//                        tools/list refresh.
//
// The Pool may be nil — every method returns (zero, ErrNotConfigured).
package mcp

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

// Store is the pgxpool-backed suite_mcp_servers accessor.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewStore constructs a Store. pool may be nil; the resulting Store's
// HasPool() reports false and every method returns ErrNotConfigured.
func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log}
}

// HasPool reports whether the Store can talk to a database. Used by the
// Pool for the "no DB" graceful-degrade path.
func (s *Store) HasPool() bool {
	return s != nil && s.pool != nil
}

// validateName mirrors the dashboard's MCPServerSchema.name constraint:
// lowercased letters, digits, dashes; 1..64 chars. Centralised here so
// the HTTP layer and the Pool see the same rules.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name required", ErrInvalidInput)
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: name exceeds 64 chars", ErrInvalidInput)
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			return fmt.Errorf(
				"%w: name must be lowercase letters / digits / dashes",
				ErrInvalidInput,
			)
		}
	}
	return nil
}

// ─── Reads ────────────────────────────────────────────────────────────────

// List returns every row visible to the current RLS context. The
// per-tenant filter is enforced by the policy on suite_mcp_servers
// (NULL tenant_id OR tenant_id = app.tenant_id), so the caller doesn't
// need to thread tenantID through this method. Pool.Reconcile passes
// app.bypass_rls=on so it sees every row.
func (s *Store) List(ctx context.Context) ([]Server, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, selectColumns+` from suite_mcp_servers order by name asc`)
	if err != nil {
		return nil, fmt.Errorf("mcp: list: %w", err)
	}
	defer rows.Close()
	out := []Server{}
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *srv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches one row by name. Returns ErrServerNotFound when no row
// matches (subject to RLS — a tenant asking for a row scoped to a
// different tenant gets ErrServerNotFound, not a permission error).
func (s *Store) Get(ctx context.Context, name string) (*Server, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		selectColumns+` from suite_mcp_servers where name = $1`, name,
	)
	if err != nil {
		return nil, fmt.Errorf("mcp: get: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrServerNotFound
	}
	return scanServer(rows)
}

// ─── Writes ───────────────────────────────────────────────────────────────

// Upsert installs (or replaces) a server row. Returns the persisted
// row. The Pool's caller is expected to invoke Reconcile after a
// successful upsert so the in-memory connection set matches.
func (s *Store) Upsert(ctx context.Context, in UpsertInput) (*Server, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if err := validateName(in.Name); err != nil {
		return nil, err
	}
	switch in.Transport {
	case TransportStdio:
		if len(in.Command) == 0 {
			return nil, fmt.Errorf(
				"%w: stdio transport requires command", ErrInvalidInput,
			)
		}
	case TransportSSE:
		if strings.TrimSpace(in.URL) == "" {
			return nil, fmt.Errorf(
				"%w: sse transport requires url", ErrInvalidInput,
			)
		}
	default:
		return nil, fmt.Errorf(
			"%w: transport must be stdio|sse", ErrInvalidInput,
		)
	}

	command := in.Command
	if command == nil {
		command = []string{}
	}
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal command: %w", err)
	}
	env := in.Env
	if env == nil {
		env = map[string]string{}
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal env: %w", err)
	}
	var (
		urlPtr      *string
		tenantPtr   *string
	)
	if in.Transport == TransportSSE {
		u := strings.TrimSpace(in.URL)
		urlPtr = &u
	}
	if in.TenantID != "" {
		t := in.TenantID
		tenantPtr = &t
	}

	const q = `
		insert into suite_mcp_servers
		    (name, transport, command, url, env, tenant_id, description,
		     is_enabled, status, last_error, tools_count,
		     installed_at, last_connected_at)
		values ($1, $2, $3, $4, $5, $6, $7,
		        true, 'connecting', null, 0,
		        now(), null)
		on conflict (name) do update set
		    transport   = excluded.transport,
		    command     = excluded.command,
		    url         = excluded.url,
		    env         = excluded.env,
		    tenant_id   = excluded.tenant_id,
		    description = excluded.description,
		    is_enabled  = true,
		    status      = 'connecting',
		    last_error  = null
		returning ` + selectFields

	rows, err := s.pool.Query(ctx, q,
		in.Name, string(in.Transport), commandJSON, urlPtr, envJSON,
		tenantPtr, in.Description,
	)
	if err != nil {
		return nil, fmt.Errorf("mcp: upsert: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, fmt.Errorf("mcp: upsert: no row returned")
	}
	return scanServer(rows)
}

// Delete removes the row. Returns ErrServerNotFound when no row matched
// (the caller can treat that as success — the Pool tears down any
// dangling adapter on the next Reconcile regardless).
func (s *Store) Delete(ctx context.Context, name string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if err := validateName(name); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `delete from suite_mcp_servers where name = $1`, name)
	if err != nil {
		return fmt.Errorf("mcp: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrServerNotFound
	}
	return nil
}

// SetEnabled flips the is_enabled flag and resets the status so the
// Pool picks it up on the next Reconcile. enabled=false moves status to
// 'disabled' synchronously so the dashboard reflects the change
// immediately; enabled=true moves status back to 'connecting' so the
// Pool re-opens.
func (s *Store) SetEnabled(ctx context.Context, name string, enabled bool) (*Server, error) {
	if !s.HasPool() {
		return nil, ErrNotConfigured
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	var status Status
	if enabled {
		status = StatusConnecting
	} else {
		status = StatusDisabled
	}
	const q = `
		update suite_mcp_servers
		   set is_enabled = $2,
		       status     = $3,
		       last_error = case when $2 then null else last_error end
		 where name = $1
		returning ` + selectFields
	rows, err := s.pool.Query(ctx, q, name, enabled, string(status))
	if err != nil {
		return nil, fmt.Errorf("mcp: set enabled: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, ErrServerNotFound
	}
	return scanServer(rows)
}

// RecordStatus persists a lifecycle transition. When status is
// StatusReady, last_connected_at is stamped to now() so the dashboard
// can show "connected 3m ago". When status is StatusErrored, errMsg is
// truncated to MaxLastErrorChars to keep the column bounded; passing ""
// clears last_error.
func (s *Store) RecordStatus(ctx context.Context, name string, status Status, errMsg string) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if err := validateName(name); err != nil {
		return err
	}
	// Truncate but don't drop completely — operators need *something*
	// in the dashboard when a connect fails.
	if len(errMsg) > MaxLastErrorChars {
		errMsg = errMsg[:MaxLastErrorChars]
	}
	var errMsgPtr *string
	if errMsg != "" {
		errMsgPtr = &errMsg
	}
	stampConnect := status == StatusReady
	_, err := s.pool.Exec(ctx, `
		update suite_mcp_servers
		   set status            = $2,
		       last_error        = case
		           when $4 then $3
		           when $2 = 'ready' then null
		           else last_error
		       end,
		       last_connected_at = case when $5 then now() else last_connected_at end
		 where name = $1
	`, name, string(status), errMsgPtr, errMsg != "", stampConnect)
	if err != nil {
		return fmt.Errorf("mcp: record status: %w", err)
	}
	return nil
}

// RecordToolsCount stamps the cached tool count + last_connected_at.
// Called by the Pool after every successful tools/list refresh.
func (s *Store) RecordToolsCount(ctx context.Context, name string, count int) error {
	if !s.HasPool() {
		return ErrNotConfigured
	}
	if err := validateName(name); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		update suite_mcp_servers
		   set tools_count       = $2,
		       last_connected_at = now()
		 where name = $1
	`, name, count)
	if err != nil {
		return fmt.Errorf("mcp: record tools count: %w", err)
	}
	return nil
}

// ─── internals ────────────────────────────────────────────────────────────

// selectFields is the explicit column list returned by RETURNING and
// the SELECT path. Centralised so a column add only changes one place.
const selectFields = `
	name, transport, command, url, env, tenant_id, description,
	is_enabled, status, last_error, tools_count,
	installed_at, last_connected_at
`

const selectColumns = `select ` + selectFields

// scanServer consumes one pgx.Rows position into a *Server. NULL columns
// land in *T fields so the wire shape emits null rather than the zero
// value.
func scanServer(rows pgx.Rows) (*Server, error) {
	var (
		name            string
		transport       string
		commandJSON     []byte
		urlVal          *string
		envJSON         []byte
		tenantID        *string
		description     string
		isEnabled       bool
		status          string
		lastError       *string
		toolsCount      int
		installedAt     time.Time
		lastConnectedAt *time.Time
	)
	if err := rows.Scan(
		&name, &transport, &commandJSON, &urlVal, &envJSON,
		&tenantID, &description, &isEnabled, &status, &lastError,
		&toolsCount, &installedAt, &lastConnectedAt,
	); err != nil {
		return nil, fmt.Errorf("mcp: scan: %w", err)
	}
	command := []string{}
	if len(commandJSON) > 0 {
		if err := json.Unmarshal(commandJSON, &command); err != nil {
			return nil, fmt.Errorf("mcp: decode command: %w", err)
		}
	}
	env := map[string]string{}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &env); err != nil {
			return nil, fmt.Errorf("mcp: decode env: %w", err)
		}
	}
	srv := &Server{
		Name:        name,
		Transport:   Transport(transport),
		Command:     command,
		URL:         urlVal,
		Env:         env,
		TenantID:    tenantID,
		Description: description,
		IsEnabled:   isEnabled,
		Status:      Status(status),
		LastError:   lastError,
		ToolsCount:  toolsCount,
		InstalledAt: installedAt.UTC().Format(time.RFC3339Nano),
	}
	if lastConnectedAt != nil {
		ts := lastConnectedAt.UTC().Format(time.RFC3339Nano)
		srv.LastConnectedAt = &ts
	}
	return srv, nil
}

// Keep errors.Is alive across refactors.
var _ = errors.Is
