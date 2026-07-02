// SPDX-License-Identifier: Apache-2.0

// Package db wires the Postgres connection pool used by the runtime.
//
// Uses pgxpool for connection pooling. Migrations run via goose against
// the same connection.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// DB is the runtime's PG handle. Wraps pgxpool with helpers.
type DB struct {
	Pool *pgxpool.Pool
}

// Config holds connection parameters.
type Config struct {
	URL             string
	MaxConnections  int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Open opens a pgxpool against the given config and verifies connectivity.
//
// Returns an error if the URL is empty or the initial ping fails.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if cfg.URL == "" {
		return nil, errors.New("db: URL is required")
	}
	pcfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("db: parse url: %w", err)
	}
	if cfg.MaxConnections > 0 {
		pcfg.MaxConns = int32(cfg.MaxConnections)
	}
	if cfg.MaxIdleConns > 0 {
		pcfg.MinConns = int32(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		pcfg.MaxConnLifetime = cfg.ConnMaxLifetime
	}

	// Bind every connection checkout to the caller's tenant for row-level
	// security. The runtime serves as a NOBYPASSRLS role, and per-tenant RLS
	// policies gate on the `app.tenant_id` GUC — but that GUC lives on the PG
	// session, while the tenant lives only in the Go context. Without this
	// hook nothing carries the tenant across, so every tenant-scoped write on
	// the bare pool fails the RLS WITH CHECK and every tenant-scoped read
	// returns zero rows.
	//
	// PrepareConn runs on each acquire with the acquiring query's context, so
	// we set the GUC deterministically from tenantctx: to the caller's tenant
	// when present, or to '' otherwise. Setting it on every acquire (rather
	// than only when a tenant is present) is what prevents a pooled connection
	// from leaking a previous borrower's tenant binding to the next caller.
	//
	// Admin/system paths that need cross-tenant visibility still open their
	// own transaction and `set local app.bypass_rls = 'on'`, which overrides
	// this per-transaction and auto-clears on commit. System writers that must
	// write a specific tenant's row from a background context bind it
	// explicitly via tenantctx.WithTenant before issuing the write.
	pcfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		if _, err := conn.Exec(ctx,
			`select set_config('app.tenant_id', $1, false)`,
			tenantctx.TenantID(ctx)); err != nil {
			// Destroy the connection and let the query retry on a fresh one;
			// a connection we can't bind is unsafe to hand out.
			return false, fmt.Errorf("db: bind tenant context: %w", err)
		}
		return true, nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// RoleSecurity describes the RLS-relevant attributes of a Postgres role.
type RoleSecurity struct {
	Name        string
	IsSuperuser bool
	BypassRLS   bool
}

// CanBypassRLS reports whether the role escapes row-level security. A
// superuser always bypasses RLS; a role with BYPASSRLS does too. Either makes
// per-tenant RLS unenforceable, so the runtime must not serve traffic as such
// a role when multi-tenancy is in play.
func (rs RoleSecurity) CanBypassRLS() bool { return rs.IsSuperuser || rs.BypassRLS }

// ConnRoleSecurity reports the RLS-relevant attributes of the role this pool
// authenticates as (current_user).
func (d *DB) ConnRoleSecurity(ctx context.Context) (RoleSecurity, error) {
	if d == nil || d.Pool == nil {
		return RoleSecurity{}, errors.New("db: not initialized")
	}
	var rs RoleSecurity
	q := `select rolname, rolsuper, rolbypassrls from pg_roles where rolname = current_user`
	if err := d.Pool.QueryRow(ctx, q).Scan(&rs.Name, &rs.IsSuperuser, &rs.BypassRLS); err != nil {
		return RoleSecurity{}, fmt.Errorf("db: read connection role security: %w", err)
	}
	return rs, nil
}

// Health pings the database with a short timeout. Returns nil on success.
func (d *DB) Health(ctx context.Context) error {
	if d == nil || d.Pool == nil {
		return errors.New("db: not initialized")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.Pool.Ping(pingCtx)
}

// Stats returns pool statistics for /metrics and dashboards.
func (d *DB) Stats() Stats {
	if d == nil || d.Pool == nil {
		return Stats{}
	}
	s := d.Pool.Stat()
	return Stats{
		AcquireCount:    s.AcquireCount(),
		AcquiredConns:   int(s.AcquiredConns()),
		IdleConns:       int(s.IdleConns()),
		TotalConns:      int(s.TotalConns()),
		MaxConns:        int(s.MaxConns()),
		NewConnsCount:   s.NewConnsCount(),
		MaxLifetimeDest: s.MaxLifetimeDestroyCount(),
	}
}

// Close releases pool resources.
func (d *DB) Close() {
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}

// Stats is a minimal stats snapshot.
type Stats struct {
	AcquireCount    int64
	AcquiredConns   int
	IdleConns       int
	TotalConns      int
	MaxConns        int
	NewConnsCount   int64
	MaxLifetimeDest int64
}