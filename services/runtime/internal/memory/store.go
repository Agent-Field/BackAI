// SPDX-License-Identifier: Apache-2.0

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const tracerName = "af-stack/memory"

// Store is the suite memory store. Construct one per process and share it.
type Store struct {
	pool     *pgxpool.Pool
	embedder Embedder
	log      *slog.Logger
	tracer   trace.Tracer

	// noEmbedderWarned guards a one-shot Warn log so a request flood
	// with Embed=true doesn't spam the slog sink when no embedder is
	// wired. Reset is unnecessary: the warning is informational.
	noEmbedderWarned sync.Once
}

// New constructs a Store.
//
// pool is required; embedder is optional. When embedder is nil, Put
// requests with Embed=true silently fall back to storing the entry
// without an embedding (a single Warn is logged the first time this
// happens). Search calls fail with a clear error in that mode because
// without an embedder we cannot translate the query string into a
// vector.
func New(pool *pgxpool.Pool, embedder Embedder, log *slog.Logger) (*Store, error) {
	if pool == nil {
		return nil, errors.New("memory: pool required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		pool:     pool,
		embedder: embedder,
		log:      log,
		tracer:   otel.Tracer(tracerName),
	}, nil
}

// HasEmbedder reports whether the store can translate text into a
// vector at runtime. Handlers use this to short-circuit Search with a
// clear 503 instead of a generic failure.
func (s *Store) HasEmbedder() bool {
	return s != nil && s.embedder != nil
}

// ─── Key + scope validation ──────────────────────────────────────────────

func validateKey(key string) error {
	if key == "" || len(key) > 512 {
		return ErrInvalidKey
	}
	return nil
}

// resolveScope normalises (scope, scope_id) for the request.
//
// global  : scope_id must be empty
// tenant  : scope_id is filled from ctx if empty; rejected if neither
//
//	the request nor ctx supplies one
//
// agent/session/run : scope_id MUST be supplied by the caller (no
//
//	fallback — the caller knows which agent / session / run owns the
//	entry, the runtime doesn't).
//
// Returns the canonical (scope, scope_id) pair to use against the DB.
func resolveScope(ctx context.Context, scope Scope, scopeID string) (Scope, string, error) {
	if !scope.Valid() {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidScope, string(scope))
	}
	scopeID = strings.TrimSpace(scopeID)
	switch scope {
	case ScopeGlobal:
		// scope_id is meaningless for global. Treat any non-empty
		// value as a caller mistake and reject it explicitly so we
		// don't silently create two visible-from-everywhere entries
		// under different scope_ids.
		if scopeID != "" {
			return "", "", fmt.Errorf("%w: scope=global must not include scope_id", ErrInvalidScope)
		}
		return scope, "", nil
	case ScopeTenant:
		// tenant_id (FORCE RLS) is the real isolation boundary now, so
		// scope=tenant's scope_id is forced to the caller's tenant — a
		// client-supplied value that names a different tenant is rejected
		// rather than trusted (that was the cross-tenant hole).
		tid := tenantctx.TenantID(ctx)
		if tid == "" {
			return "", "", fmt.Errorf("%w: scope=tenant requires tenant context", ErrInvalidScope)
		}
		if scopeID != "" && scopeID != tid {
			return "", "", fmt.Errorf("%w: scope=tenant scope_id must equal the caller's tenant", ErrInvalidScope)
		}
		return scope, tid, nil
	case ScopeAgent, ScopeSession, ScopeRun:
		if scopeID == "" {
			return "", "", fmt.Errorf("%w: scope=%s requires scope_id", ErrInvalidScope, scope)
		}
		return scope, scopeID, nil
	}
	return "", "", ErrInvalidScope
}

// ─── Get ──────────────────────────────────────────────────────────────────

// Get returns the entry for (scope, scope_id, key). Returns
// ErrNotFound when missing, ErrInvalidScope / ErrInvalidKey when the
// inputs fail validation.
func (s *Store) Get(ctx context.Context, scope Scope, scopeID, key string) (Entry, error) {
	ctx, span := s.tracer.Start(ctx, "memory.get", trace.WithAttributes(
		attribute.String("memory.scope", string(scope)),
		attribute.String("memory.scope_id", scopeID),
		attribute.String("memory.key", key),
	))
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return Entry{}, err
	}
	scope, scopeID, err := resolveScope(ctx, scope, scopeID)
	if err != nil {
		span.RecordError(err)
		return Entry{}, err
	}

	row := s.pool.QueryRow(ctx, `
		select scope, scope_id, key, value, metadata,
		       (embedding is not null) as has_embedding,
		       created_at, updated_at
		from suite_memory
		where scope = $1 and scope_id = $2 and key = $3
	`, string(scope), scopeID, key)

	entry, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		span.RecordError(err)
		return Entry{}, fmt.Errorf("memory: get: %w", err)
	}
	return entry, nil
}

// ─── Put (upsert) ─────────────────────────────────────────────────────────

// Put inserts or updates an entry. When in.Embed is true and an
// embedder is configured, the value is embedded and stored alongside
// the entry; if the embedder errors, the entry is still written
// (without the embedding) and a Warn is logged. When no embedder is
// configured, Embed=true degrades silently to a no-embedding write.
func (s *Store) Put(ctx context.Context, in PutInput) (Entry, error) {
	ctx, span := s.tracer.Start(ctx, "memory.put", trace.WithAttributes(
		attribute.String("memory.scope", string(in.Scope)),
		attribute.String("memory.scope_id", in.ScopeID),
		attribute.String("memory.key", in.Key),
		attribute.Bool("memory.embed", in.Embed),
	))
	defer span.End()

	if err := validateKey(in.Key); err != nil {
		span.RecordError(err)
		return Entry{}, err
	}
	scope, scopeID, err := resolveScope(ctx, in.Scope, in.ScopeID)
	if err != nil {
		span.RecordError(err)
		return Entry{}, err
	}
	// Every row is partitioned by tenant_id (FORCE RLS). A write needs a
	// resolved tenant so it can be attributed and pass the RLS with-check.
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Entry{}, ErrTenantRequired
	}

	valueJSON, err := normaliseJSON(in.Value)
	if err != nil {
		span.RecordError(err)
		return Entry{}, fmt.Errorf("memory: encode value: %w", err)
	}
	metaJSON, err := encodeMetadata(in.Metadata)
	if err != nil {
		span.RecordError(err)
		return Entry{}, fmt.Errorf("memory: encode metadata: %w", err)
	}

	var embeddingArg any
	if in.Embed {
		switch {
		case s.embedder == nil:
			s.noEmbedderWarned.Do(func() {
				s.log.Warn("memory: embed requested but no embedder configured; entries will be stored without embeddings")
			})
		default:
			vec, embedErr := s.embedder.Embed(ctx, embedText(valueJSON))
			switch {
			case embedErr != nil:
				s.log.Warn("memory: embedder failed; entry will be stored without embedding",
					"scope", string(scope),
					"scope_id", scopeID,
					"key", in.Key,
					"error", embedErr,
				)
			case len(vec) != EmbeddingDim:
				s.log.Error("memory: embedder returned wrong dimension; entry will be stored without embedding",
					"scope", string(scope),
					"scope_id", scopeID,
					"key", in.Key,
					"got", len(vec),
					"want", EmbeddingDim,
				)
				return Entry{}, ErrEmbeddingDimMismatch
			default:
				embeddingArg = encodeVector(vec)
			}
		}
	}

	row := s.pool.QueryRow(ctx, `
		insert into suite_memory
			(tenant_id, scope, scope_id, key, value, metadata, embedding, created_at, updated_at)
		values
			($1, $2, $3, $4, $5, $6, $7, now(), now())
		on conflict (tenant_id, scope, scope_id, key) do update set
			value      = excluded.value,
			metadata   = excluded.metadata,
			embedding  = coalesce(excluded.embedding, suite_memory.embedding),
			updated_at = now()
		returning scope, scope_id, key, value, metadata,
		          (embedding is not null) as has_embedding,
		          created_at, updated_at
	`, tenantID, string(scope), scopeID, in.Key, valueJSON, metaJSON, embeddingArg)

	entry, err := scanEntry(row)
	if err != nil {
		span.RecordError(err)
		return Entry{}, fmt.Errorf("memory: put: %w", err)
	}
	return entry, nil
}

// ─── Delete ───────────────────────────────────────────────────────────────

// Delete removes an entry. Returns ErrNotFound if no row matched.
func (s *Store) Delete(ctx context.Context, scope Scope, scopeID, key string) error {
	ctx, span := s.tracer.Start(ctx, "memory.delete", trace.WithAttributes(
		attribute.String("memory.scope", string(scope)),
		attribute.String("memory.scope_id", scopeID),
		attribute.String("memory.key", key),
	))
	defer span.End()

	if err := validateKey(key); err != nil {
		span.RecordError(err)
		return err
	}
	scope, scopeID, err := resolveScope(ctx, scope, scopeID)
	if err != nil {
		span.RecordError(err)
		return err
	}

	tag, err := s.pool.Exec(ctx, `
		delete from suite_memory where scope = $1 and scope_id = $2 and key = $3
	`, string(scope), scopeID, key)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("memory: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── List ─────────────────────────────────────────────────────────────────

// List returns a paginated view of entries. opts.Scope and
// opts.ScopeID act as exact-match filters when set; opts.Prefix
// further narrows the key. Defaults: limit=50, offset=0; clamped to
// [1,200].
func (s *Store) List(ctx context.Context, opts ListOpts) (List, error) {
	ctx, span := s.tracer.Start(ctx, "memory.list", trace.WithAttributes(
		attribute.String("memory.scope", string(opts.Scope)),
		attribute.String("memory.scope_id", opts.ScopeID),
		attribute.String("memory.prefix", opts.Prefix),
	))
	defer span.End()

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	// Build the where clause from optional filters. We do this with a
	// hand-rolled positional-arg list rather than squirrel/goqu to
	// avoid pulling another dependency; the filter set is tiny.
	conds := []string{}
	args := []any{}
	if opts.Scope != "" {
		// Resolve the scope so callers can filter by `scope=tenant` and
		// have it auto-fill from request context just like Put.
		scope, scopeID, err := resolveScope(ctx, opts.Scope, opts.ScopeID)
		// Tolerate the "scope=tenant without scope_id and no tenant
		// in ctx" case in List specifically: handlers may want to
		// scan every tenant-scoped entry from an admin context. We
		// only enforce the scope_id requirement for agent/session/run
		// where there's no sensible cross-scope query.
		switch {
		case err == nil:
			conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
			args = append(args, string(scope))
			if scopeID != "" {
				conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
				args = append(args, scopeID)
			}
		case errors.Is(err, ErrInvalidScope) && (opts.Scope == ScopeTenant || opts.Scope == ScopeGlobal):
			// Fall back to a scope-only filter for permissive admin
			// scans. The handler layer can still 400 if it wants.
			conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
			args = append(args, string(opts.Scope))
		default:
			span.RecordError(err)
			return List{Entries: []Entry{}}, err
		}
	}
	if opts.Prefix != "" {
		conds = append(conds, fmt.Sprintf("key like $%d", len(args)+1))
		args = append(args, opts.Prefix+"%")
	}

	where := ""
	if len(conds) > 0 {
		where = "where " + strings.Join(conds, " and ")
	}

	// The operator List (dashboard browse) runs with no tenant bound, so
	// FORCE RLS would return zero rows. With no tenant context, open a
	// bypass-RLS transaction to browse cross-tenant; when a tenant IS
	// bound, run under normal RLS so the list is scoped to that tenant.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		span.RecordError(err)
		return List{Entries: []Entry{}}, fmt.Errorf("memory: list begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if tenantctx.TenantID(ctx) == "" {
		if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
			span.RecordError(err)
			return List{Entries: []Entry{}}, fmt.Errorf("memory: list bypass: %w", err)
		}
	}

	var total int
	if err := tx.QueryRow(ctx, fmt.Sprintf(`select count(*) from suite_memory %s`, where), args...).Scan(&total); err != nil {
		span.RecordError(err)
		return List{Entries: []Entry{}}, fmt.Errorf("memory: list count: %w", err)
	}

	listArgs := append([]any{}, args...)
	listArgs = append(listArgs, limit, offset)
	rows, err := tx.Query(ctx, fmt.Sprintf(`
		select scope, scope_id, key, value, metadata,
		       (embedding is not null) as has_embedding,
		       created_at, updated_at
		from suite_memory
		%s
		order by scope, scope_id, key
		limit $%d offset $%d
	`, where, len(args)+1, len(args)+2), listArgs...)
	if err != nil {
		span.RecordError(err)
		return List{Entries: []Entry{}}, fmt.Errorf("memory: list query: %w", err)
	}
	defer rows.Close()

	out := List{Entries: []Entry{}, Total: total}
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			span.RecordError(scanErr)
			return List{Entries: []Entry{}}, fmt.Errorf("memory: list scan: %w", scanErr)
		}
		out.Entries = append(out.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return List{Entries: []Entry{}}, fmt.Errorf("memory: list iter: %w", err)
	}
	out.HasMore = offset+len(out.Entries) < total
	return out, nil
}

// ─── Search ───────────────────────────────────────────────────────────────

// Search embeds req.Query and returns the top_k entries ordered by
// cosine similarity. Optional req.Scope / req.ScopeID restrict the
// search to a subset; req.Threshold filters out hits whose similarity
// (1 - distance) falls below the threshold.
//
// When no embedder is configured, the search short-circuits to an
// empty result and returns an error — searching without an embedder
// can't be done sensibly, even partially.
func (s *Store) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	ctx, span := s.tracer.Start(ctx, "memory.search", trace.WithAttributes(
		attribute.String("memory.scope", string(req.Scope)),
		attribute.String("memory.scope_id", req.ScopeID),
		attribute.Int("memory.top_k", req.TopK),
	))
	defer span.End()

	if strings.TrimSpace(req.Query) == "" {
		return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: query required")
	}
	if s.embedder == nil {
		return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: search requires an embedder")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}

	startedAt := time.Now()

	vec, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		span.RecordError(err)
		return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: embed query: %w", err)
	}
	if len(vec) != EmbeddingDim {
		return SearchResult{Hits: []SearchHit{}}, ErrEmbeddingDimMismatch
	}

	conds := []string{"embedding is not null"}
	args := []any{encodeVector(vec)}
	if req.Scope != "" {
		scope, scopeID, scopeErr := resolveScope(ctx, req.Scope, req.ScopeID)
		switch {
		case scopeErr == nil:
			conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
			args = append(args, string(scope))
			if scopeID != "" {
				conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
				args = append(args, scopeID)
			}
		case errors.Is(scopeErr, ErrInvalidScope) && (req.Scope == ScopeTenant || req.Scope == ScopeGlobal):
			conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
			args = append(args, string(req.Scope))
		default:
			span.RecordError(scopeErr)
			return SearchResult{Hits: []SearchHit{}}, scopeErr
		}
	}
	where := "where " + strings.Join(conds, " and ")

	args = append(args, topK)
	q := fmt.Sprintf(`
		select scope, scope_id, key, value, metadata,
		       (embedding is not null) as has_embedding,
		       created_at, updated_at,
		       (embedding <=> $1::vector) as distance
		from suite_memory
		%s
		order by embedding <=> $1::vector
		limit $%d
	`, where, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		span.RecordError(err)
		return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: search query: %w", err)
	}
	defer rows.Close()

	out := SearchResult{Hits: []SearchHit{}}
	for rows.Next() {
		entry, distance, scanErr := scanSearchHit(rows)
		if scanErr != nil {
			span.RecordError(scanErr)
			return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: search scan: %w", scanErr)
		}
		similarity := 1.0 - distance
		if req.Threshold != nil && similarity < *req.Threshold {
			continue
		}
		out.Hits = append(out.Hits, SearchHit{
			Entry:      entry,
			Similarity: similarity,
			Distance:   distance,
		})
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return SearchResult{Hits: []SearchHit{}}, fmt.Errorf("memory: search iter: %w", err)
	}
	out.DurationMS = time.Since(startedAt).Milliseconds()
	return out, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// rowScanner is satisfied by *pgxpool.Rows and pgx.Row so scanEntry can
// be shared between Query and QueryRow paths.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEntry reads the standard 8-column select into an Entry. The
// caller is responsible for the column order matching the select
// statement; every call site in this file uses the same projection.
func scanEntry(r rowScanner) (Entry, error) {
	var (
		scope        string
		scopeID      string
		key          string
		valueBytes   []byte
		metaBytes    []byte
		hasEmbedding bool
		createdAt    time.Time
		updatedAt    time.Time
	)
	if err := r.Scan(&scope, &scopeID, &key, &valueBytes, &metaBytes, &hasEmbedding, &createdAt, &updatedAt); err != nil {
		return Entry{}, err
	}
	return buildEntry(scope, scopeID, key, valueBytes, metaBytes, hasEmbedding, createdAt, updatedAt), nil
}

// scanSearchHit adds a trailing distance column to the Entry scan.
func scanSearchHit(r rowScanner) (Entry, float64, error) {
	var (
		scope        string
		scopeID      string
		key          string
		valueBytes   []byte
		metaBytes    []byte
		hasEmbedding bool
		createdAt    time.Time
		updatedAt    time.Time
		distance     float64
	)
	if err := r.Scan(&scope, &scopeID, &key, &valueBytes, &metaBytes, &hasEmbedding, &createdAt, &updatedAt, &distance); err != nil {
		return Entry{}, 0, err
	}
	return buildEntry(scope, scopeID, key, valueBytes, metaBytes, hasEmbedding, createdAt, updatedAt), distance, nil
}

// buildEntry maps raw column values to the wire shape. scopeID is
// returned as nil-pointer when the underlying string is empty so the
// dashboard's `scope_id: z.string().nullable()` parses against
// `null` rather than `""` — the latter would tripwire the schema if a
// future revision tightens to `.min(1).nullable()`.
func buildEntry(
	scope, scopeID, key string,
	valueBytes, metaBytes []byte,
	hasEmbedding bool,
	createdAt, updatedAt time.Time,
) Entry {
	var scopeIDPtr *string
	if scopeID != "" {
		s := scopeID
		scopeIDPtr = &s
	}
	metadata := map[string]any{}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	value := json.RawMessage(valueBytes)
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	return Entry{
		Scope:        Scope(scope),
		ScopeID:      scopeIDPtr,
		Key:          key,
		Value:        value,
		Metadata:     metadata,
		HasEmbedding: hasEmbedding,
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:    updatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// normaliseJSON validates that raw is well-formed JSON. nil / empty
// is coerced to `null` so the JSONB column is never asked to parse an
// empty string.
func normaliseJSON(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("null"), nil
	}
	// json.Valid is the cheapest way to validate without round-tripping
	// through a generic decoder.
	if !json.Valid(raw) {
		return nil, fmt.Errorf("not valid json")
	}
	return raw, nil
}

// encodeMetadata marshals the metadata map to JSON. nil and empty maps
// both encode as `{}` so the metadata column always holds an object.
func encodeMetadata(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// encodeVector serialises vec into the textual form pgvector accepts in
// a query parameter cast to ::vector. Format: `[v1,v2,...]`.
//
// We don't pull in pgvector-go because the text form round-trips
// losslessly and keeps the dependency tree small. The pool driver
// (pgx) treats the value as a plain string; the SQL `$N::vector`
// cast in the calling statement does the rest.
func encodeVector(vec []float32) string {
	var sb strings.Builder
	sb.Grow(len(vec)*9 + 2)
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// embedText converts the stored JSON value into the text that gets
// embedded. We use the raw JSON byte representation — that preserves
// the structure (so {"name": "Acme"} and "Acme" embed differently)
// without forcing a stringify rule on the caller. Strings, objects,
// arrays, numbers all flow through the same path.
func embedText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Strip JSON-string quoting so a stored value of `"hello world"`
	// embeds as `hello world` rather than `"hello world"`. Other JSON
	// types pass through verbatim.
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	return string(raw)
}
