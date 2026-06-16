// SPDX-License-Identifier: Apache-2.0

package llmcache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/pricing"
)

// ErrNotFound is returned by Get when no entry matches. Callers should
// treat this as a miss and proceed to the upstream call.
var ErrNotFound = errors.New("llmcache: not found")

// Entry is the materialised cache row returned by Get.
//
// ResponseJSON is the raw JSON bytes of the upstream response — exactly
// what was stored on Put. Callers re-emit it verbatim so the cached
// reply is byte-identical to a fresh one (modulo the cache headers the
// gateway adds).
type Entry struct {
	Key              string
	TenantID         string
	Model            string
	RequestHash      string
	ResponseJSON     []byte
	PromptTokens     int
	CompletionTokens int
	Hits             int
	CostSavedUSD     float64
	CreatedAt        time.Time
	ExpiresAt        time.Time
	LastHitAt        *time.Time
}

// CacheStats mirrors apps/dashboard/src/lib/api.ts CacheStatsSchema so
// the handler can marshal directly without an adapter struct.
//
// Fields:
//
//	TotalCalls  = CacheHits + CacheMisses (the runtime tracks misses
//	              separately in the gateway request log; for now Stats
//	              reports persisted hits only and the caller fills in
//	              misses if it has them).
//	HitRate     = CacheHits / TotalCalls, 0..1
//	SavingsUSD  = sum of cost_saved_usd over all rows for this tenant
//	Entries     = live row count (excludes expired rows)
type CacheStats struct {
	TotalCalls  int64   `json:"total_calls"`
	CacheHits   int64   `json:"cache_hits"`
	CacheMisses int64   `json:"cache_misses"`
	HitRate     float64 `json:"hit_rate"`
	SavingsUSD  float64 `json:"savings_usd"`
	Entries     int64   `json:"entries"`
}

// FlushOpts scopes a manual cache flush. Empty fields mean match all.
type FlushOpts struct {
	TenantID   string
	PromptHash string
}

// Cache is the PG-backed exact-match LLM response cache.
type Cache struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	// now is overridable in tests so expiry behaviour can be exercised
	// without sleeping. Defaults to time.Now in New.
	now func() time.Time
}

// New constructs a Cache. pool must be non-nil.
func New(pool *pgxpool.Pool, log *slog.Logger) (*Cache, error) {
	if pool == nil {
		return nil, errors.New("llmcache: pool required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Cache{
		pool: pool,
		log:  log,
		now:  time.Now,
	}, nil
}

// Get looks up an unexpired entry for (tenantID, model, requestHash).
// On hit it atomically increments `hits`, updates `last_hit_at`, and
// adds the would-have-been upstream cost to `cost_saved_usd` so the
// running savings total stays accurate even when the pricing table
// changes between hits.
//
// Returns (entry, true, nil) on hit, (nil, false, nil) on miss (no row
// or expired). A non-nil error indicates a real database problem.
func (c *Cache) Get(ctx context.Context, tenantID, model, requestHash string) (*Entry, bool, error) {
	key := CacheKey(tenantID, model, requestHash)
	now := c.now().UTC()

	// We do the read + update in one statement so concurrent gateway
	// workers can't race on the hit counter. The RETURNING clause gives
	// us the post-update row in a single round-trip.
	const q = `
update suite_llm_cache
   set hits = hits + 1,
       last_hit_at = $2,
       cost_saved_usd = cost_saved_usd + $3
 where key = $1
   and expires_at > $2
returning key, tenant_id, model, request_hash, response_json,
          prompt_tokens, completion_tokens, hits, cost_saved_usd,
          created_at, expires_at, last_hit_at
`

	// First read tokens so we can compute the cost saved for THIS hit.
	// We can't do "set cost_saved_usd = cost_saved_usd + price(tokens)"
	// in pure SQL without embedding the pricing table in PG, so we do a
	// pre-flight read for the token counts and pass the dollar amount
	// in as a parameter.
	var promptTokens, completionTokens int
	err := c.pool.QueryRow(ctx, `
select prompt_tokens, completion_tokens
  from suite_llm_cache
 where key = $1 and expires_at > $2
`, key, now).Scan(&promptTokens, &completionTokens)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("llmcache: get preflight: %w", err)
	}

	costSaved, _ := pricing.EstimateCostUSD(model, promptTokens, completionTokens)

	var e Entry
	err = c.pool.QueryRow(ctx, q, key, now, costSaved).Scan(
		&e.Key,
		&tenantIDScanner{dst: &e.TenantID},
		&e.Model,
		&e.RequestHash,
		&e.ResponseJSON,
		&e.PromptTokens,
		&e.CompletionTokens,
		&e.Hits,
		&e.CostSavedUSD,
		&e.CreatedAt,
		&e.ExpiresAt,
		&e.LastHitAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Race: row expired between pre-flight read and update.
			// Treat as a miss; the gateway will recompute and Put again.
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("llmcache: get update: %w", err)
	}
	return &e, true, nil
}

// Put inserts (or refreshes) a cache entry for the given key.
//
// On conflict (same key already exists) the response, tokens, and
// expires_at are overwritten — this is what we want when an upstream
// re-call would have replaced the stored response anyway (e.g. the
// previous entry just expired). Hit counters are preserved across
// updates so stats stay continuous even if a tenant's response stream
// flips between miss and hit over time.
//
// ttl must be > 0. A non-positive ttl returns an error rather than
// silently writing an entry that's already expired.
func (c *Cache) Put(
	ctx context.Context,
	tenantID, model, requestHash string,
	response []byte,
	promptTokens, completionTokens int,
	ttl time.Duration,
) error {
	if ttl <= 0 {
		return errors.New("llmcache: ttl must be positive")
	}
	if model == "" {
		return errors.New("llmcache: model required")
	}
	if len(response) == 0 {
		return errors.New("llmcache: response must be non-empty")
	}

	key := CacheKey(tenantID, model, requestHash)
	now := c.now().UTC()
	expiresAt := now.Add(ttl)

	var tenantPtr any
	if tenantID != "" {
		tenantPtr = tenantID
	}

	const q = `
insert into suite_llm_cache
    (key, tenant_id, model, request_hash, response_json,
     prompt_tokens, completion_tokens, expires_at, created_at)
values
    ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
on conflict (key) do update set
    response_json     = excluded.response_json,
    prompt_tokens     = excluded.prompt_tokens,
    completion_tokens = excluded.completion_tokens,
    expires_at        = excluded.expires_at
`
	if _, err := c.pool.Exec(ctx, q,
		key,
		tenantPtr,
		model,
		requestHash,
		string(response),
		promptTokens,
		completionTokens,
		expiresAt,
		now,
	); err != nil {
		return fmt.Errorf("llmcache: put: %w", err)
	}
	return nil
}

// Stats returns aggregate counters for a tenant. Empty tenantID means
// "across all tenants" — this is what the dashboard's admin view uses.
//
// CacheMisses is not stored in this table (misses don't write rows);
// callers that want a full hit-rate should pass the miss count from
// their gateway request log via the calling handler. For convenience
// Stats returns 0 for misses and hit_rate computed against hits-only,
// which is what an isolated cache caller wants.
func (c *Cache) Stats(ctx context.Context, tenantID string) (CacheStats, error) {
	now := c.now().UTC()
	const q = `
select coalesce(sum(hits), 0)::bigint           as hits,
       coalesce(sum(cost_saved_usd), 0)::float8 as savings,
       count(*)::bigint                          as entries
  from suite_llm_cache
 where expires_at > $1
   and ($2::uuid is null or tenant_id = $2::uuid)
`
	var tenantParam any
	if tenantID != "" {
		tenantParam = tenantID
	}
	var out CacheStats
	if err := c.pool.QueryRow(ctx, q, now, tenantParam).Scan(
		&out.CacheHits, &out.SavingsUSD, &out.Entries,
	); err != nil {
		return CacheStats{}, fmt.Errorf("llmcache: stats: %w", err)
	}
	out.TotalCalls = out.CacheHits + out.CacheMisses
	if out.TotalCalls > 0 {
		out.HitRate = float64(out.CacheHits) / float64(out.TotalCalls)
	}
	return out, nil
}

// Evict deletes rows whose expires_at has passed. Returns the number of
// rows removed. Safe to call concurrently with Get/Put; the only effect
// of a race is that an in-flight Get on an expired row sees the row
// disappear and returns a miss instead of a hit.
func (c *Cache) Evict(ctx context.Context) (int, error) {
	now := c.now().UTC()
	tag, err := c.pool.Exec(ctx, `
delete from suite_llm_cache
 where expires_at < $1
`, now)
	if err != nil {
		return 0, fmt.Errorf("llmcache: evict: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// Flush deletes live and expired cache rows matching the supplied filters.
// Empty filters flush the entire cache.
func (c *Cache) Flush(ctx context.Context, opts FlushOpts) (int64, error) {
	if opts.TenantID == "" && opts.PromptHash == "" {
		var count int64
		if err := c.pool.QueryRow(ctx, `select count(*)::bigint from suite_llm_cache`).Scan(&count); err != nil {
			return 0, fmt.Errorf("llmcache: flush count: %w", err)
		}
		if _, err := c.pool.Exec(ctx, `truncate table suite_llm_cache`); err != nil {
			return 0, fmt.Errorf("llmcache: flush: %w", err)
		}
		return count, nil
	}

	conds := []string{"1=1"}
	args := []any{}
	if opts.TenantID != "" {
		args = append(args, opts.TenantID)
		conds = append(conds, fmt.Sprintf("tenant_id = $%d::uuid", len(args)))
	}
	if opts.PromptHash != "" {
		args = append(args, opts.PromptHash)
		conds = append(conds, fmt.Sprintf("request_hash = $%d", len(args)))
	}
	where := strings.Join(conds, " and ")
	q := `
with batch as (
	select key from suite_llm_cache
	 where ` + where + `
	 limit 1000
)
delete from suite_llm_cache
 where key in (select key from batch)
`
	var deleted int64
	for {
		tag, err := c.pool.Exec(ctx, q, args...)
		if err != nil {
			return deleted, fmt.Errorf("llmcache: flush: %w", err)
		}
		n := tag.RowsAffected()
		deleted += n
		if n < 1000 {
			break
		}
	}
	return deleted, nil
}

// tenantIDScanner adapts a nullable uuid column into a string field.
// When the column is NULL we leave the destination empty rather than
// fail the scan.
type tenantIDScanner struct {
	dst *string
}

// Scan implements pgx.Scanner indirectly via the sql.Scanner interface
// pgx supports for custom destinations.
func (t *tenantIDScanner) Scan(src any) error {
	if src == nil {
		*t.dst = ""
		return nil
	}
	switch v := src.(type) {
	case string:
		*t.dst = v
		return nil
	case []byte:
		*t.dst = string(v)
		return nil
	case [16]byte:
		// pgx returns UUIDs as [16]byte when no specific type is hinted.
		// Format as canonical 8-4-4-4-12 hex.
		*t.dst = formatUUID(v)
		return nil
	default:
		return fmt.Errorf("llmcache: unsupported tenant_id scan type %T", src)
	}
}

// formatUUID renders a 16-byte uuid as the standard 8-4-4-4-12 hex form.
func formatUUID(b [16]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	pos := 0
	for i, by := range b {
		out[pos] = hex[by>>4]
		out[pos+1] = hex[by&0x0f]
		pos += 2
		if i == 3 || i == 5 || i == 7 || i == 9 {
			out[pos] = '-'
			pos++
		}
	}
	return string(out)
}
