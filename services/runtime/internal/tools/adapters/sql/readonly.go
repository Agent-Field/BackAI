// SPDX-License-Identifier: Apache-2.0

// Package sql implements the read-only SQL ToolAdapter. It wraps a
// pgxpool.Pool and runs caller-provided SELECT/WITH/EXPLAIN queries
// inside a transaction with `SET LOCAL transaction_read_only = on`.
//
// Tenant isolation is enforced two ways: the runtime's tenant context
// is bound via `set_config('app.tenant_id', ...)` and PG RLS policies
// applied to every tenant-scoped table will reject cross-tenant reads.
// The adapter rejects any query containing mutating keywords or
// multiple statements before even reaching the DB.
//
// One verb: query(sql, args?, max_rows?). max_rows defaults to 50 and is
// capped at 200 to keep response payloads bounded.
package sql

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrInvalidArgs is returned for malformed input.
var (
	ErrInvalidArgs    = errors.New("sql: invalid arguments")
	ErrNotReadOnly    = errors.New("sql: only SELECT/WITH/EXPLAIN are permitted")
	ErrMultiStatement = errors.New("sql: multiple statements are not allowed")
	ErrNotConfigured  = errors.New("sql: pool not configured")
)

// Adapter wraps a pgxpool.Pool.
type Adapter struct {
	pool *pgxpool.Pool
}

// New constructs an Adapter. Nil pool = unconfigured.
func New(pool *pgxpool.Pool) *Adapter { return &Adapter{pool: pool} }

// ID returns the adapter identifier.
func (a *Adapter) ID() string { return "readonly-sql" }

// Configured reports whether a pool is wired.
func (a *Adapter) Configured() bool { return a != nil && a.pool != nil }

// QueryResult is the return shape from Query: columns + row maps.
type QueryResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

// blockedSQL flags any mutating verb at word boundaries. Belt-and-
// braces: even though we set transaction_read_only, an attacker may
// abuse a SECURITY DEFINER function.
var blockedSQL = regexp.MustCompile(`(?i)\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|call|do|vacuum|analyze|refresh|listen|notify|set|reset)\b`)

// Query executes one read-only statement with the given args, returning
// up to maxRows rows. tenantID is bound on the session before the query
// runs so PG RLS applies.
func (a *Adapter) Query(ctx context.Context, tenantID, sqlText string, args []any, maxRows int) (QueryResult, error) {
	if !a.Configured() {
		return QueryResult{}, ErrNotConfigured
	}
	cleaned, err := validateReadOnly(sqlText)
	if err != nil {
		return QueryResult{}, err
	}
	if maxRows <= 0 {
		maxRows = 50
	}
	if maxRows > 200 {
		maxRows = 200
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return QueryResult{}, fmt.Errorf("sql: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local transaction_read_only = on"); err != nil {
		return QueryResult{}, fmt.Errorf("sql: enforce read-only: %w", err)
	}
	if tenantID != "" {
		if _, err := tx.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
			return QueryResult{}, fmt.Errorf("sql: tenant context: %w", err)
		}
	}
	rows, err := tx.Query(ctx, cleaned, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}
	out := QueryResult{Columns: columns, Rows: []map[string]any{}}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return QueryResult{}, err
		}
		row := map[string]any{}
		for i, v := range vals {
			row[columns[i]] = normalize(v)
		}
		out.Rows = append(out.Rows, row)
		if len(out.Rows) >= maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, err
	}
	return out, nil
}

func validateReadOnly(sqlText string) (string, error) {
	trimmed := strings.TrimSpace(sqlText)
	trimmed = strings.TrimSuffix(trimmed, ";")
	if trimmed == "" {
		return "", fmt.Errorf("%w: sql required", ErrInvalidArgs)
	}
	if strings.Contains(trimmed, ";") {
		return "", ErrMultiStatement
	}
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(lower, "select ") || strings.HasPrefix(lower, "with ") || strings.HasPrefix(lower, "explain ") || strings.HasPrefix(lower, "select\n") || strings.HasPrefix(lower, "with\n") || strings.HasPrefix(lower, "explain\n")) {
		return "", ErrNotReadOnly
	}
	if blockedSQL.MatchString(lower) {
		return "", ErrNotReadOnly
	}
	return trimmed, nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return x
	}
}
