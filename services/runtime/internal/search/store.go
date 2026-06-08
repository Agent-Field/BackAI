// SPDX-License-Identifier: Apache-2.0

package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const tracerName = "af-stack/search"

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type dbPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool     dbPool
	embedder Embedder
	log      *slog.Logger
	tracer   trace.Tracer
}

func New(pool *pgxpool.Pool, embedder Embedder, log *slog.Logger) (*Store, error) {
	if pool == nil {
		return nil, errors.New("search: pool required")
	}
	return NewWithPool(pool, embedder, log), nil
}

func NewWithPool(pool dbPool, embedder Embedder, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{
		pool:     pool,
		embedder: embedder,
		log:      log,
		tracer:   otel.Tracer(tracerName),
	}
}

func (s *Store) HasEmbedder() bool {
	return s != nil && s.embedder != nil
}

func (s *Store) Upsert(ctx context.Context, in UpsertInput) (Document, error) {
	ctx, span := s.tracer.Start(ctx, "search.upsert", trace.WithAttributes(
		attribute.String("search.namespace", in.Namespace),
		attribute.String("search.key", in.Key),
		attribute.Bool("search.embed", in.Embed),
	))
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Document{}, ErrTenantRequired
	}
	namespace := normaliseNamespace(in.Namespace)
	if err := validateNamespace(namespace); err != nil {
		return Document{}, err
	}
	if err := validateKey(in.Key); err != nil {
		return Document{}, err
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Body) == "" {
		return Document{}, fmt.Errorf("%w: title or body is required", ErrValidation)
	}
	metadata, err := normaliseMetadata(in.Metadata)
	if err != nil {
		return Document{}, fmt.Errorf("%w: metadata must be JSON-serializable", ErrValidation)
	}

	var embeddingArg any
	if in.Embed {
		if s.embedder == nil {
			s.log.Warn("search: embed requested but no embedder configured; document will remain FTS-only",
				"namespace", namespace, "key", in.Key)
		} else {
			vec, embedErr := s.embedder.Embed(ctx, embedText(in.Title, in.Body))
			if embedErr != nil {
				s.log.Warn("search: embedder failed; document will remain FTS-only",
					"namespace", namespace, "key", in.Key, "error", embedErr)
			} else if len(vec) != EmbeddingDim {
				return Document{}, ErrEmbeddingDimMismatch
			} else {
				embeddingArg = encodeVector(vec)
			}
		}
	}

	row := s.pool.QueryRow(ctx, `
		insert into suite_search_documents
			(tenant_id, namespace, key, title, body, metadata, embedding, created_at, updated_at)
		values
			($1, $2, $3, $4, $5, $6, $7, now(), now())
		on conflict (tenant_id, namespace, key) do update set
			title = excluded.title,
			body = excluded.body,
			metadata = excluded.metadata,
			embedding = coalesce(excluded.embedding, suite_search_documents.embedding),
			updated_at = now()
		returning tenant_id::text, namespace, key, title, body, metadata,
		          (embedding is not null) as has_embedding,
		          created_at, updated_at
	`, tenantID, namespace, in.Key, in.Title, in.Body, metadata, embeddingArg)

	doc, err := scanDocument(row)
	if err != nil {
		span.RecordError(err)
		return Document{}, fmt.Errorf("search: upsert: %w", err)
	}
	return doc, nil
}

func (s *Store) Delete(ctx context.Context, in DeleteInput) error {
	ctx, span := s.tracer.Start(ctx, "search.delete", trace.WithAttributes(
		attribute.String("search.namespace", in.Namespace),
		attribute.String("search.key", in.Key),
	))
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return ErrTenantRequired
	}
	namespace := normaliseNamespace(in.Namespace)
	if err := validateNamespace(namespace); err != nil {
		return err
	}
	if err := validateKey(in.Key); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		delete from suite_search_documents
		where tenant_id = $1 and namespace = $2 and key = $3
	`, tenantID, namespace, in.Key)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("search: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Search(ctx context.Context, req Request) (Result, error) {
	ctx, span := s.tracer.Start(ctx, "search.search", trace.WithAttributes(
		attribute.String("search.mode", string(req.Mode.Canonical())),
		attribute.String("search.namespace", req.Namespace),
		attribute.Int("search.limit", req.Limit),
	))
	defer span.End()

	startedAt := time.Now()
	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Result{Hits: []Hit{}, Mode: req.Mode.Canonical()}, ErrTenantRequired
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		return Result{Hits: []Hit{}, Mode: req.Mode.Canonical()}, fmt.Errorf("%w: query is required", ErrValidation)
	}
	if !req.Mode.Valid() {
		return Result{Hits: []Hit{}, Mode: req.Mode}, fmt.Errorf("%w: mode must be fts, vector, or hybrid", ErrValidation)
	}
	mode := req.Mode.Canonical()
	if req.Namespace != "" {
		req.Namespace = normaliseNamespace(req.Namespace)
		if err := validateNamespace(req.Namespace); err != nil {
			return Result{Hits: []Hit{}, Mode: mode}, err
		}
	}
	limit := clampLimit(req.Limit)
	offset := clampOffset(req.Offset)

	var hits []Hit
	var err error
	switch mode {
	case ModeFTS:
		hits, err = s.searchFTS(ctx, tenantID, req, limit, offset)
	case ModeVector:
		hits, err = s.searchVector(ctx, tenantID, req, limit, offset)
	case ModeHybrid:
		hits, err = s.searchHybrid(ctx, tenantID, req, limit, offset)
	default:
		err = fmt.Errorf("%w: unsupported mode", ErrValidation)
	}
	if err != nil {
		span.RecordError(err)
		return Result{Hits: []Hit{}, Mode: mode}, err
	}
	return Result{Hits: hits, Mode: mode, DurationMS: time.Since(startedAt).Milliseconds()}, nil
}

func (s *Store) searchFTS(ctx context.Context, tenantID string, req Request, limit, offset int) ([]Hit, error) {
	filter, err := newFilterBuilder(tenantID, req)
	if err != nil {
		return nil, err
	}
	queryPos := filter.add(req.Query)
	limitPos := filter.add(limit)
	offsetPos := filter.add(offset)
	args := filter.args
	sql := fmt.Sprintf(`
		with q as (select websearch_to_tsquery('simple', $%d) as query)
		select tenant_id::text, namespace, key, title, body, metadata,
		       (embedding is not null) as has_embedding,
		       created_at, updated_at,
		       ts_rank_cd(search_vector, q.query) as fts_rank
		from suite_search_documents, q
		where %s and search_vector @@ q.query
		order by fts_rank desc, updated_at desc
		limit $%d offset $%d
	`, queryPos, filter.where(), limitPos, offsetPos)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search: fts query: %w", err)
	}
	defer rows.Close()
	return scanHits(rows, true, false)
}

func (s *Store) searchVector(ctx context.Context, tenantID string, req Request, limit, offset int) ([]Hit, error) {
	if s.embedder == nil {
		return nil, ErrEmbedderNotConfigured
	}
	vec, err := s.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("search: embed query: %w", err)
	}
	if len(vec) != EmbeddingDim {
		return nil, ErrEmbeddingDimMismatch
	}
	filter, err := newFilterBuilder(tenantID, req)
	if err != nil {
		return nil, err
	}
	vecPos := filter.add(encodeVector(vec))
	limitPos := filter.add(limit)
	offsetPos := filter.add(offset)
	args := filter.args
	sql := fmt.Sprintf(`
		select tenant_id::text, namespace, key, title, body, metadata,
		       (embedding is not null) as has_embedding,
		       created_at, updated_at,
		       (1 - (embedding <=> $%d::vector)) as similarity
		from suite_search_documents
		where %s and embedding is not null
		order by embedding <=> $%d::vector, updated_at desc
		limit $%d offset $%d
	`, vecPos, filter.where(), vecPos, limitPos, offsetPos)
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search: vector query: %w", err)
	}
	defer rows.Close()
	return scanHits(rows, false, true)
}

func (s *Store) searchHybrid(ctx context.Context, tenantID string, req Request, limit, offset int) ([]Hit, error) {
	candidateLimit := limit + offset
	if candidateLimit < 20 {
		candidateLimit = 20
	}
	if candidateLimit > 200 {
		candidateLimit = 200
	}

	ftsHits, err := s.searchFTS(ctx, tenantID, req, candidateLimit, 0)
	if err != nil {
		return nil, err
	}
	if s.embedder == nil {
		return windowHits(ftHitsWithScores(ftsHits), limit, offset), nil
	}
	vectorHits, err := s.searchVector(ctx, tenantID, req, candidateLimit, 0)
	if err != nil {
		return nil, err
	}
	return windowHits(mergeHybrid(ftsHits, vectorHits), limit, offset), nil
}

type filterBuilder struct {
	conds []string
	args  []any
}

func newFilterBuilder(tenantID string, req Request) (*filterBuilder, error) {
	f := &filterBuilder{}
	f.conds = append(f.conds, fmt.Sprintf("tenant_id = $%d", f.add(tenantID)))
	if req.Namespace != "" {
		f.conds = append(f.conds, fmt.Sprintf("namespace = $%d", f.add(req.Namespace)))
	}
	if len(req.MetadataFilter) > 0 {
		meta, err := normaliseMetadata(req.MetadataFilter)
		if err != nil {
			return nil, fmt.Errorf("%w: metadata_filter must be JSON-serializable", ErrValidation)
		}
		f.conds = append(f.conds, fmt.Sprintf("metadata @> $%d::jsonb", f.add(string(meta))))
	}
	return f, nil
}

func (f *filterBuilder) add(v any) int {
	f.args = append(f.args, v)
	return len(f.args)
}

func (f *filterBuilder) where() string {
	return strings.Join(f.conds, " and ")
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(r rowScanner) (Document, error) {
	var (
		tenantID     string
		namespace    string
		key          string
		title        string
		body         string
		metaBytes    []byte
		hasEmbedding bool
		createdAt    time.Time
		updatedAt    time.Time
	)
	if err := r.Scan(&tenantID, &namespace, &key, &title, &body, &metaBytes, &hasEmbedding, &createdAt, &updatedAt); err != nil {
		return Document{}, err
	}
	metadata := map[string]any{}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return Document{
		TenantID:     tenantID,
		Namespace:    namespace,
		Key:          key,
		Title:        title,
		Body:         body,
		Metadata:     metadata,
		HasEmbedding: hasEmbedding,
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:    updatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func scanHits(rows pgx.Rows, hasFTS, hasVector bool) ([]Hit, error) {
	hits := []Hit{}
	for rows.Next() {
		doc, score, err := scanHit(rows, hasFTS, hasVector)
		if err != nil {
			return []Hit{}, err
		}
		hits = append(hits, docWithScore(doc, score, hasFTS, hasVector))
	}
	if err := rows.Err(); err != nil {
		return []Hit{}, err
	}
	return hits, nil
}

func scanHit(r rowScanner, hasFTS, hasVector bool) (Document, float64, error) {
	var (
		tenantID     string
		namespace    string
		key          string
		title        string
		body         string
		metaBytes    []byte
		hasEmbedding bool
		createdAt    time.Time
		updatedAt    time.Time
		score        float64
	)
	if err := r.Scan(&tenantID, &namespace, &key, &title, &body, &metaBytes, &hasEmbedding, &createdAt, &updatedAt, &score); err != nil {
		return Document{}, 0, err
	}
	metadata := map[string]any{}
	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return Document{
		TenantID:     tenantID,
		Namespace:    namespace,
		Key:          key,
		Title:        title,
		Body:         body,
		Metadata:     metadata,
		HasEmbedding: hasEmbedding,
		CreatedAt:    createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:    updatedAt.UTC().Format(time.RFC3339Nano),
	}, score, nil
}

func docWithScore(doc Document, score float64, hasFTS, hasVector bool) Hit {
	hit := Hit{Document: doc, Score: score}
	if hasFTS {
		v := score
		hit.FTSRank = &v
	}
	if hasVector {
		v := score
		hit.Similarity = &v
	}
	return hit
}

func mergeHybrid(ftsHits, vectorHits []Hit) []Hit {
	type acc struct {
		hit        Hit
		ftsRank    int
		vectorRank int
	}
	byKey := map[string]*acc{}
	for i, h := range ftsHits {
		k := docKey(h.Document)
		byKey[k] = &acc{hit: h, ftsRank: i + 1}
	}
	for i, h := range vectorHits {
		k := docKey(h.Document)
		a := byKey[k]
		if a == nil {
			byKey[k] = &acc{hit: h, vectorRank: i + 1}
			continue
		}
		a.vectorRank = i + 1
		a.hit.Document = h.Document
		a.hit.Similarity = h.Similarity
	}
	out := make([]Hit, 0, len(byKey))
	for _, a := range byKey {
		score := 0.0
		if a.ftsRank > 0 {
			score += reciprocalRank(a.ftsRank)
		}
		if a.vectorRank > 0 {
			score += reciprocalRank(a.vectorRank)
		}
		a.hit.Score = score
		out = append(out, a.hit)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Document.UpdatedAt > out[j].Document.UpdatedAt
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func ftHitsWithScores(hits []Hit) []Hit {
	for i := range hits {
		hits[i].Score = reciprocalRank(i + 1)
	}
	return hits
}

func windowHits(hits []Hit, limit, offset int) []Hit {
	if offset >= len(hits) {
		return []Hit{}
	}
	end := offset + limit
	if end > len(hits) {
		end = len(hits)
	}
	return hits[offset:end]
}

func reciprocalRank(rank int) float64 {
	return 1.0 / float64(60+rank)
}

func docKey(doc Document) string {
	return doc.TenantID + "\x00" + doc.Namespace + "\x00" + doc.Key
}

func validateNamespace(namespace string) error {
	if namespace == "" || len(namespace) > 128 {
		return fmt.Errorf("%w: namespace must be 1-128 characters", ErrValidation)
	}
	for _, r := range namespace {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return fmt.Errorf("%w: namespace contains invalid character %q", ErrValidation, r)
	}
	return nil
}

func validateKey(key string) error {
	if key == "" || len(key) > 512 {
		return fmt.Errorf("%w: key must be 1-512 characters", ErrValidation)
	}
	return nil
}

func normaliseNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return DefaultNamespace
	}
	return namespace
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func embedText(title, body string) string {
	switch {
	case title != "" && body != "":
		return title + "\n\n" + body
	case title != "":
		return title
	default:
		return body
	}
}

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
