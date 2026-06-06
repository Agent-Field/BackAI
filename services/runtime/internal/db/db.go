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

	"github.com/jackc/pgx/v5/pgxpool"
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
