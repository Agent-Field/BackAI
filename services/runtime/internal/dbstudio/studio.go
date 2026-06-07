// SPDX-License-Identifier: Apache-2.0

// Package dbstudio is the read-mostly DB-introspection + SQL-runner backend
// for the AF Stack dashboard's "Database" tab (Phase 8.1).
//
// The Studio struct wraps a *pgxpool.Pool and exposes four operations:
//
//   - ListTables  — every user table/view/matview with row-estimate + size
//   - Table       — columns, indexes, RLS policies for a single relation
//   - Rows        — paginated `SELECT *` returning a SQLRunResult
//   - RunSQL      — arbitrary SQL with a read-only safety wrapper
//
// All output shapes are encoded with snake_case JSON tags so the wire
// format matches the zod schemas in apps/dashboard/src/lib/api.ts exactly
// (DBTableSchema, DBTableDetailSchema, SQLRunResultSchema). REST handlers
// in services/runtime/internal/server/dbstudio.go forward our return
// values directly to writeJSON; do not add server-only fields here.
//
// Safety notes
//
// Identifiers (schema, table, column names) used in dynamically built SQL
// MUST be escaped via pgx.Identifier.Sanitize() — never via fmt.Sprintf
// or string concatenation. Studio.validateIdent guards every Rows() entry
// point with a strict ^[a-zA-Z_][a-zA-Z0-9_]*$ regex and returns
// ErrInvalidIdentifier for anything else. Sanitize() then handles
// double-quoting and embedded quote escapes for the small subset that
// does match.
//
// RunSQL with readOnly=true wraps the user statement in a read-only
// transaction with a 15s statement_timeout. Postgres itself enforces the
// read-only restriction — any write attempt surfaces as
// "cannot execute <X> in a read-only transaction" which we translate to
// ErrReadOnlyViolation. The handler maps that to a READ_ONLY_VIOLATION
// error envelope. Admin-only writes (readOnly=false) are wrapped in a
// plain transaction; Phase 8 documents this gate but the handler should
// still call into RunSQL when the caller is allowed.
package dbstudio

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the OTel tracer name. Spans are named dbstudio.<op>.
const tracerName = "af-stack/dbstudio"

// Limits.
const (
	defaultRowLimit = 500
	maxRowLimit     = 10000
	readOnlyTimeout = 15 * time.Second
)

// systemSchemas are excluded from ListTables. Anything else (public,
// extensions a user installed, app-created schemas) is included.
var systemSchemas = map[string]struct{}{
	"pg_catalog":         {},
	"information_schema": {},
	"pg_toast":           {},
}

// identRe is the strict identifier shape Studio accepts in Rows() and
// any other path that interpolates the name into SQL. Anything outside
// this set is rejected as ErrInvalidIdentifier — even though
// pgx.Identifier.Sanitize() would also escape it, we want to fail
// loudly rather than silently letting weird names through.
var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Sentinel errors. Handlers translate these to error envelopes:
//
//	ErrInvalidIdentifier -> INVALID_IDENTIFIER (400)
//	ErrReadOnlyViolation -> READ_ONLY_VIOLATION (400)
//	ErrEmptyStatement    -> VALIDATION_FAILED (400)
var (
	ErrInvalidIdentifier = errors.New("dbstudio: invalid identifier")
	ErrReadOnlyViolation = errors.New("dbstudio: read-only violation")
	ErrEmptyStatement    = errors.New("dbstudio: empty statement")
)

// Studio is the dbstudio service handle. Construct one per process with
// New; methods are safe for concurrent use (pgxpool handles pooling).
type Studio struct {
	pool *pgxpool.Pool
}

// New constructs a Studio around the given pool. nil pool returns nil.
func New(pool *pgxpool.Pool) *Studio {
	if pool == nil {
		return nil
	}
	return &Studio{pool: pool}
}

func (s *Studio) tracer() trace.Tracer { return otel.Tracer(tracerName) }

// ─── Wire shapes ──────────────────────────────────────────────────────────
//
// Every field's JSON tag must match the corresponding zod field name in
// apps/dashboard/src/lib/api.ts. Adding a field here without also
// extending the schema breaks the dashboard's safeParse.

// DBTable matches DBTableSchema.
type DBTable struct {
	Schema        string `json:"schema"`
	Name          string `json:"name"`
	Kind          string `json:"kind"` // table | view | matview
	EstimatedRows int64  `json:"estimated_rows"`
	SizeBytes     int64  `json:"size_bytes"`
	HasRLS        bool   `json:"has_rls"`
}

// DBColumn matches DBColumnSchema.
type DBColumn struct {
	Name         string  `json:"name"`
	DataType     string  `json:"data_type"`
	IsNullable   bool    `json:"is_nullable"`
	DefaultValue *string `json:"default_value"`
	IsPrimaryKey bool    `json:"is_primary_key"`
	IsUnique     bool    `json:"is_unique"`
}

// DBIndex matches DBIndexSchema.
type DBIndex struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	IsUnique   bool   `json:"is_unique"`
	IsPrimary  bool   `json:"is_primary"`
}

// RLSPolicy matches RLSPolicySchema. UsingExpression and
// WithCheckExpression are nullable per zod; we emit JSON null via
// pointer-to-string.
type RLSPolicy struct {
	Name                string   `json:"name"`
	Cmd                 string   `json:"cmd"` // SELECT | INSERT | UPDATE | DELETE | ALL
	Permissive          bool     `json:"permissive"`
	Roles               []string `json:"roles"`
	UsingExpression     *string  `json:"using_expression"`
	WithCheckExpression *string  `json:"with_check_expression"`
}

// DBTableDetail matches DBTableDetailSchema.
type DBTableDetail struct {
	Schema     string      `json:"schema"`
	Name       string      `json:"name"`
	Kind       string      `json:"kind"`
	Columns    []DBColumn  `json:"columns"`
	Indexes    []DBIndex   `json:"indexes"`
	RLSEnabled bool        `json:"rls_enabled"`
	RLSForced  bool        `json:"rls_forced"`
	Policies   []RLSPolicy `json:"policies"`
}

// SQLRunResult matches SQLRunResultSchema. Rows is `[][]any` to support
// any JSON-encodable column value; pgx scans typed values into `any`
// which serialise correctly for primitive types.
type SQLRunResult struct {
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"row_count"`
	Truncated  bool     `json:"truncated"`
	DurationMS int64    `json:"duration_ms"`
}

// ─── ListTables ───────────────────────────────────────────────────────────

// ListTables enumerates every user table/view/matview the runtime can
// see, with PG's row estimate, total size, and RLS-enabled flag.
//
// We use the catalog tables directly (pg_class + pg_namespace) rather
// than information_schema because:
//   - reltuples + relrowsecurity are not exposed via information_schema
//   - pg_total_relation_size needs an oid
//
// Views and materialized views return 0 for size and rows (PG doesn't
// track these the same way).
func (s *Studio) ListTables(ctx context.Context) ([]DBTable, error) {
	ctx, span := s.tracer().Start(ctx, "dbstudio.list_tables")
	defer span.End()

	const q = `
		select
			n.nspname                                as schema,
			c.relname                                as name,
			case c.relkind
				when 'r' then 'table'
				when 'p' then 'table'
				when 'v' then 'view'
				when 'm' then 'matview'
			end                                      as kind,
			case when c.relkind in ('r','p','m')
				then greatest(c.reltuples::bigint, 0)
				else 0::bigint end                   as estimated_rows,
			case when c.relkind in ('r','p','m')
				then pg_total_relation_size(c.oid)
				else 0::bigint end                   as size_bytes,
			coalesce(c.relrowsecurity, false)        as has_rls
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where c.relkind in ('r','p','v','m')
		  and n.nspname not in ('pg_catalog','information_schema','pg_toast')
		  and n.nspname not like 'pg_temp_%'
		  and n.nspname not like 'pg_toast_temp_%'
		order by n.nspname, c.relname
	`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("dbstudio: list tables query: %w", err)
	}
	defer rows.Close()

	out := make([]DBTable, 0, 32)
	for rows.Next() {
		var t DBTable
		if err := rows.Scan(&t.Schema, &t.Name, &t.Kind, &t.EstimatedRows, &t.SizeBytes, &t.HasRLS); err != nil {
			return nil, fmt.Errorf("dbstudio: list tables scan: %w", err)
		}
		// Belt + braces: skip system schemas that snuck through (e.g.
		// PG ever changes the system list). The SQL filter already
		// handles the standard cases.
		if _, ok := systemSchemas[t.Schema]; ok {
			continue
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dbstudio: list tables iterate: %w", err)
	}
	span.SetAttributes(attribute.Int("dbstudio.tables", len(out)))
	return out, nil
}

// ─── Table ────────────────────────────────────────────────────────────────

// Table returns the full schema detail for one relation: columns, PK
// flags, unique flags, indexes, RLS state, and policies.
//
// Schema + name are validated before any query — we use them as bound
// parameters where possible, but column-flag lookups need to filter
// by name so identifier validation is the only line of defence.
func (s *Studio) Table(ctx context.Context, schema, name string) (DBTableDetail, error) {
	ctx, span := s.tracer().Start(ctx, "dbstudio.table",
		trace.WithAttributes(
			attribute.String("dbstudio.schema", schema),
			attribute.String("dbstudio.table", name),
		),
	)
	defer span.End()

	if err := s.validateIdent(schema); err != nil {
		return DBTableDetail{}, err
	}
	if err := s.validateIdent(name); err != nil {
		return DBTableDetail{}, err
	}

	detail := DBTableDetail{
		Schema:   schema,
		Name:     name,
		Columns:  []DBColumn{},
		Indexes:  []DBIndex{},
		Policies: []RLSPolicy{},
	}

	// 1) Relation existence + kind + RLS flags from pg_class.
	var relkind string
	const relQ = `
		select
			case c.relkind
				when 'r' then 'table'
				when 'p' then 'table'
				when 'v' then 'view'
				when 'm' then 'matview'
				else c.relkind::text
			end as kind,
			coalesce(c.relrowsecurity, false),
			coalesce(c.relforcerowsecurity, false)
		from pg_class c
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = $1 and c.relname = $2
	`
	if err := s.pool.QueryRow(ctx, relQ, schema, name).Scan(
		&relkind, &detail.RLSEnabled, &detail.RLSForced,
	); err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: table lookup: %w", err)
	}
	detail.Kind = relkind

	// 2) Primary-key columns (set, for the columns query below).
	pkCols, err := s.constraintCols(ctx, schema, name, "PRIMARY KEY")
	if err != nil {
		return DBTableDetail{}, err
	}
	uqCols, err := s.constraintCols(ctx, schema, name, "UNIQUE")
	if err != nil {
		return DBTableDetail{}, err
	}

	// 3) Columns.
	const colQ = `
		select
			column_name,
			coalesce(data_type, ''),
			is_nullable = 'YES',
			column_default
		from information_schema.columns
		where table_schema = $1 and table_name = $2
		order by ordinal_position
	`
	colRows, err := s.pool.Query(ctx, colQ, schema, name)
	if err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: columns query: %w", err)
	}
	for colRows.Next() {
		var c DBColumn
		var def *string
		if err := colRows.Scan(&c.Name, &c.DataType, &c.IsNullable, &def); err != nil {
			colRows.Close()
			return DBTableDetail{}, fmt.Errorf("dbstudio: columns scan: %w", err)
		}
		c.DefaultValue = def
		if _, ok := pkCols[c.Name]; ok {
			c.IsPrimaryKey = true
		}
		if _, ok := uqCols[c.Name]; ok {
			c.IsUnique = true
		}
		detail.Columns = append(detail.Columns, c)
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: columns iterate: %w", err)
	}

	// 4) Indexes from pg_indexes. The indexdef column already contains
	// the CREATE INDEX statement which is what the dashboard displays.
	// is_unique + is_primary need a join into pg_index.
	const idxQ = `
		select
			i.indexname,
			i.indexdef,
			coalesce(ix.indisunique, false) as is_unique,
			coalesce(ix.indisprimary, false) as is_primary
		from pg_indexes i
		join pg_class c on c.relname = i.indexname
		join pg_namespace nc on nc.oid = c.relnamespace and nc.nspname = i.schemaname
		join pg_index ix on ix.indexrelid = c.oid
		where i.schemaname = $1 and i.tablename = $2
		order by i.indexname
	`
	idxRows, err := s.pool.Query(ctx, idxQ, schema, name)
	if err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: indexes query: %w", err)
	}
	for idxRows.Next() {
		var ix DBIndex
		if err := idxRows.Scan(&ix.Name, &ix.Definition, &ix.IsUnique, &ix.IsPrimary); err != nil {
			idxRows.Close()
			return DBTableDetail{}, fmt.Errorf("dbstudio: indexes scan: %w", err)
		}
		detail.Indexes = append(detail.Indexes, ix)
	}
	idxRows.Close()
	if err := idxRows.Err(); err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: indexes iterate: %w", err)
	}

	// 5) RLS policies from pg_policies (already decodes the pg_node_tree
	// internal representation into legible SQL strings).
	const polQ = `
		select
			policyname,
			cmd,
			permissive = 'PERMISSIVE',
			coalesce(roles, '{}')::text[],
			qual,
			with_check
		from pg_policies
		where schemaname = $1 and tablename = $2
		order by policyname
	`
	polRows, err := s.pool.Query(ctx, polQ, schema, name)
	if err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: policies query: %w", err)
	}
	for polRows.Next() {
		var p RLSPolicy
		var using, withCheck *string
		var roles []string
		if err := polRows.Scan(&p.Name, &p.Cmd, &p.Permissive, &roles, &using, &withCheck); err != nil {
			polRows.Close()
			return DBTableDetail{}, fmt.Errorf("dbstudio: policies scan: %w", err)
		}
		p.Roles = roles
		if p.Roles == nil {
			p.Roles = []string{}
		}
		p.UsingExpression = using
		p.WithCheckExpression = withCheck
		// Normalise cmd to the api.ts enum set.
		switch strings.ToUpper(p.Cmd) {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "ALL":
			p.Cmd = strings.ToUpper(p.Cmd)
		default:
			p.Cmd = "ALL"
		}
		detail.Policies = append(detail.Policies, p)
	}
	polRows.Close()
	if err := polRows.Err(); err != nil {
		return DBTableDetail{}, fmt.Errorf("dbstudio: policies iterate: %w", err)
	}

	span.SetAttributes(
		attribute.Int("dbstudio.columns", len(detail.Columns)),
		attribute.Int("dbstudio.indexes", len(detail.Indexes)),
		attribute.Int("dbstudio.policies", len(detail.Policies)),
	)
	return detail, nil
}

// constraintCols returns the set of column names participating in any
// PRIMARY KEY / UNIQUE constraint on the given table. We aggregate
// across constraints — a column that's part of either a PK or a
// composite UNIQUE shows up here.
func (s *Studio) constraintCols(
	ctx context.Context, schema, name, kind string,
) (map[string]struct{}, error) {
	const q = `
		select kcu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
			on  kcu.constraint_schema = tc.constraint_schema
			and kcu.constraint_name = tc.constraint_name
		where tc.table_schema = $1
		  and tc.table_name = $2
		  and tc.constraint_type = $3
	`
	rows, err := s.pool.Query(ctx, q, schema, name, kind)
	if err != nil {
		return nil, fmt.Errorf("dbstudio: constraint cols (%s): %w", kind, err)
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, fmt.Errorf("dbstudio: constraint scan: %w", err)
		}
		out[col] = struct{}{}
	}
	return out, rows.Err()
}

// ─── Rows ─────────────────────────────────────────────────────────────────

// Rows returns up to `limit` rows from (schema, table) starting at
// `offset`, returning a SQLRunResult shaped like RunSQL.
//
// Identifiers are double-validated:
//  1. validateIdent rejects anything outside ^[a-zA-Z_][a-zA-Z0-9_]*$
//     with ErrInvalidIdentifier.
//  2. pgx.Identifier{schema, table}.Sanitize() wraps the legitimate
//     name in proper double quotes (and escapes any embedded quotes).
//
// Limit defaults to 100 (max 1000 per api.ts) and offset clamps to 0.
func (s *Studio) Rows(
	ctx context.Context, schema, table string, limit, offset int,
) (SQLRunResult, error) {
	ctx, span := s.tracer().Start(ctx, "dbstudio.rows",
		trace.WithAttributes(
			attribute.String("dbstudio.schema", schema),
			attribute.String("dbstudio.table", table),
			attribute.Int("dbstudio.limit", limit),
			attribute.Int("dbstudio.offset", offset),
		),
	)
	defer span.End()

	if err := s.validateIdent(schema); err != nil {
		return SQLRunResult{}, err
	}
	if err := s.validateIdent(table); err != nil {
		return SQLRunResult{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	qualified := pgx.Identifier{schema, table}.Sanitize()
	stmt := fmt.Sprintf("select * from %s limit $1 offset $2", qualified)

	start := time.Now()
	rows, err := s.pool.Query(ctx, stmt, limit, offset)
	if err != nil {
		return SQLRunResult{}, fmt.Errorf("dbstudio: rows query: %w", err)
	}
	defer rows.Close()

	result, err := scanResult(rows, limit)
	if err != nil {
		return SQLRunResult{}, fmt.Errorf("dbstudio: rows scan: %w", err)
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result, nil
}

// ─── RunSQL ───────────────────────────────────────────────────────────────

// RunSQL executes one or more SQL statements with optional read-only
// safety. Postgres itself enforces read-only — any write attempt
// surfaces as "cannot execute <X> in a read-only transaction" which we
// translate to ErrReadOnlyViolation.
//
// readOnly=true (the default for the dashboard's SQL panel):
//
//	begin;
//	  set local statement_timeout = '15s';
//	  set transaction read only;
//	  <user statement>
//	rollback;
//
// readOnly=false (admin/MT-admin gated path):
//
//	begin;
//	  set local statement_timeout = '15s';
//	  <user statement>
//	commit;
//
// limit defaults to 500 and caps at 10000 (api.ts contract).
func (s *Studio) RunSQL(
	ctx context.Context, statement string, readOnly bool, limit int,
) (SQLRunResult, error) {
	ctx, span := s.tracer().Start(ctx, "dbstudio.sql",
		trace.WithAttributes(
			attribute.Bool("dbstudio.read_only", readOnly),
			attribute.Int("dbstudio.limit", limit),
		),
	)
	defer span.End()

	statement = strings.TrimSpace(statement)
	if statement == "" {
		return SQLRunResult{}, ErrEmptyStatement
	}
	if limit <= 0 {
		limit = defaultRowLimit
	}
	if limit > maxRowLimit {
		limit = maxRowLimit
	}

	// Apply an outer timeout slightly above the per-statement timeout so
	// the PG-side timer wins and we surface the friendly error rather
	// than a ctx-cancelled wrapped error.
	ctx, cancel := context.WithTimeout(ctx, readOnlyTimeout+5*time.Second)
	defer cancel()

	start := time.Now()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SQLRunResult{}, fmt.Errorf("dbstudio: begin tx: %w", err)
	}
	// On any error we rollback; on success readOnly=false commits and
	// readOnly=true also rollbacks. The defer here covers the error path
	// AND the readOnly=true happy path — `_ = tx.Rollback()` after a
	// successful Commit is a no-op in pgx.
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "set local statement_timeout = '15s'"); err != nil {
		return SQLRunResult{}, fmt.Errorf("dbstudio: set timeout: %w", err)
	}
	if readOnly {
		if _, err := tx.Exec(ctx, "set transaction read only"); err != nil {
			return SQLRunResult{}, fmt.Errorf("dbstudio: set read only: %w", err)
		}
	}

	rows, err := tx.Query(ctx, statement)
	if err != nil {
		if isReadOnlyViolation(err) {
			return SQLRunResult{}, ErrReadOnlyViolation
		}
		return SQLRunResult{}, fmt.Errorf("dbstudio: exec: %w", err)
	}

	result, scanErr := scanResult(rows, limit)
	rows.Close()
	// Any error surfaced AFTER pgx accepted the query (e.g. write
	// attempts in a DO block, or a row-level constraint) also needs the
	// read-only translation.
	if scanErr == nil {
		if rerr := rows.Err(); rerr != nil {
			scanErr = rerr
		}
	}
	if scanErr != nil {
		if isReadOnlyViolation(scanErr) {
			return SQLRunResult{}, ErrReadOnlyViolation
		}
		return SQLRunResult{}, fmt.Errorf("dbstudio: scan: %w", scanErr)
	}

	if !readOnly {
		if err := tx.Commit(ctx); err != nil {
			return SQLRunResult{}, fmt.Errorf("dbstudio: commit: %w", err)
		}
	}

	result.DurationMS = time.Since(start).Milliseconds()
	span.SetAttributes(
		attribute.Int("dbstudio.row_count", result.RowCount),
		attribute.Bool("dbstudio.truncated", result.Truncated),
	)
	return result, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// validateIdent enforces the strict identifier regex used by Rows() and
// Table(). Returns ErrInvalidIdentifier for anything else so the
// handler can surface a clean INVALID_IDENTIFIER envelope.
func (s *Studio) validateIdent(id string) error {
	if id == "" || len(id) > 63 {
		return ErrInvalidIdentifier
	}
	if !identRe.MatchString(id) {
		return ErrInvalidIdentifier
	}
	return nil
}

// ValidateIdent exposes the same check publicly so the REST handler
// can pre-validate URL path segments before constructing internal
// arguments.
func ValidateIdent(id string) error {
	if id == "" || len(id) > 63 {
		return ErrInvalidIdentifier
	}
	if !identRe.MatchString(id) {
		return ErrInvalidIdentifier
	}
	return nil
}

// scanResult reads up to `limit` rows from a pgx.Rows iterator and
// returns a SQLRunResult. If the cursor still has rows after `limit`,
// Truncated is set true.
//
// Values are scanned via RawValues + FieldDescriptions to populate
// `[]any` directly — pgx's native row.Values() does the right thing
// for primitive types (numbers, strings, booleans, JSON) and falls
// back to []byte for unrecognised PG types, which json.Marshal
// renders as base64.
func scanResult(rows pgx.Rows, limit int) (SQLRunResult, error) {
	out := SQLRunResult{
		Columns: []string{},
		Rows:    [][]any{},
	}
	desc := rows.FieldDescriptions()
	for _, fd := range desc {
		out.Columns = append(out.Columns, string(fd.Name))
	}
	for rows.Next() {
		if len(out.Rows) >= limit {
			out.Truncated = true
			// Keep draining? No — pgx.Rows lets us bail by returning;
			// the caller closes the cursor. Bailing here lets large
			// result sets exit without buffering the rest in memory.
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return SQLRunResult{}, err
		}
		// pgx returns some types as []byte (e.g. unknown OIDs); these
		// JSON-marshal as base64 which is fine for a debug UI. We
		// don't try to be clever about coercion here.
		out.Rows = append(out.Rows, vals)
	}
	out.RowCount = len(out.Rows)
	return out, nil
}

// isReadOnlyViolation detects PG's "cannot execute X in a read-only
// transaction" error. We match on the message because pgx propagates
// the PgError code (25006) but also because non-pgx wrappers might
// strip it.
func isReadOnlyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pge *pgconn.PgError
	// pgx exposes PgError via errors.As in v5 (under pgconn).
	if errors.As(err, &pge) {
		// 25006 = read_only_sql_transaction (per
		// https://www.postgresql.org/docs/current/errcodes-appendix.html).
		if pge.Code == "25006" {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "read-only transaction") ||
		strings.Contains(msg, "read only transaction")
}