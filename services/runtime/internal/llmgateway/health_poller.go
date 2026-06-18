// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderHealthPoller records periodic LiteLLM health observations into
// suite_provider_health_log for the admin dashboard.
type ProviderHealthPoller struct {
	pool      *pgxpool.Pool
	baseURL   string
	masterKey string
	log       *slog.Logger
	client    *http.Client
	mu        sync.Mutex
}

type ProviderHealthSnapshot struct {
	Provider        string         `json:"provider"`
	Status          string         `json:"status"`
	AvailabilityPct float64        `json:"availability_pct"`
	Observations    int            `json:"observations"`
	MedianLatencyMS int            `json:"median_latency_ms"`
	P95LatencyMS    int            `json:"p95_latency_ms"`
	LastObservedAt  *string        `json:"last_observed_at"`
	LatencyBuckets  map[string]int `json:"latency_buckets"`
}

type ProviderHealthList struct {
	Providers []ProviderHealthSnapshot `json:"providers"`
	Window    string                   `json:"window"`
}

func NewProviderHealthPoller(pool *pgxpool.Pool, baseURL, masterKey string, log *slog.Logger) *ProviderHealthPoller {
	if pool == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &ProviderHealthPoller{
		pool:      pool,
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		masterKey: masterKey,
		log:       log,
		client:    &http.Client{Timeout: 55 * time.Second},
	}
}

func (p *ProviderHealthPoller) Configured() bool {
	return p != nil && p.pool != nil && p.baseURL != ""
}

func (p *ProviderHealthPoller) Run(ctx context.Context, interval time.Duration) {
	if !p.Configured() {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	p.poll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *ProviderHealthPoller) poll(ctx context.Context) {
	if !p.mu.TryLock() {
		p.log.Warn("provider health poll skipped; previous poll still running")
		return
	}
	defer p.mu.Unlock()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil {
		return
	}
	if p.masterKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.masterKey)
	}
	resp, err := p.client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		// Gateway unreachable — emit a single sentinel row so the
		// dashboard can render "gateway down" rather than nothing.
		p.insertRow(ctx, "litellm", "unhealthy", latency, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		p.log.Warn("provider health poll skipped; LiteLLM rejected master key",
			"status_code", resp.StatusCode)
		return
	}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) == nil {
		// Group LiteLLM's per-model endpoint state by upstream provider
		// (openrouter, anthropic, openai, ...) so Zone A shows real
		// upstream signal rather than a single "litellm" aggregate.
		perProvider := groupEndpointsByProvider(parsed)
		if len(perProvider) > 0 {
			for prov, group := range perProvider {
				status := perProviderStatus(group)
				details := map[string]any{
					"healthy_endpoint_count":   group.healthy,
					"unhealthy_endpoint_count": group.unhealthy,
					"status_code":              resp.StatusCode,
					"models":                   group.models,
				}
				p.insertRow(ctx, prov, status, latency, details)
			}
			return
		}
	}

	// Fall back to the previous aggregate row when parsing didn't yield
	// per-provider data (older LiteLLM versions, unexpected schema, etc.).
	status := "healthy"
	details := map[string]any{}
	if parsed != nil {
		details = parsed
	} else if len(body) > 0 {
		details["body"] = string(body)
	}
	details["status_code"] = resp.StatusCode
	if resp.StatusCode >= 500 {
		status = "unhealthy"
	} else if resp.StatusCode >= 300 {
		status = "degraded"
	}
	p.insertRow(ctx, "litellm", status, latency, details)
}

func (p *ProviderHealthPoller) insertRow(ctx context.Context, provider, status string, latency int64, details map[string]any) {
	detailsJSON, _ := json.Marshal(details)
	if _, err := p.pool.Exec(ctx, `
		insert into suite_provider_health_log (provider, status, latency_ms, details)
		values ($1, $2, $3, $4::jsonb)
	`, provider, status, latency, detailsJSON); err != nil {
		p.log.Warn("provider health insert failed", "error", err, "provider", provider)
	}
}

// providerEndpointGroup tracks endpoint counts and the model names that
// belong to a single upstream provider during one poll cycle.
type providerEndpointGroup struct {
	healthy   int
	unhealthy int
	models    []string
}

// groupEndpointsByProvider scans LiteLLM's /health payload (which
// reports healthy_endpoints + unhealthy_endpoints arrays of per-model
// objects) and folds the results into one bucket per upstream provider.
func groupEndpointsByProvider(parsed map[string]any) map[string]*providerEndpointGroup {
	out := map[string]*providerEndpointGroup{}
	addAll := func(key, status string) {
		arr, _ := parsed[key].([]any)
		for _, item := range arr {
			row, _ := item.(map[string]any)
			modelName, _ := row["model"].(string)
			provider := providerForModel(modelName)
			if provider == "" {
				continue
			}
			group, ok := out[provider]
			if !ok {
				group = &providerEndpointGroup{}
				out[provider] = group
			}
			if status == "healthy" {
				group.healthy++
			} else {
				group.unhealthy++
			}
			group.models = append(group.models, modelName)
		}
	}
	addAll("healthy_endpoints", "healthy")
	addAll("unhealthy_endpoints", "unhealthy")
	return out
}

// providerForModel maps a LiteLLM model identifier (e.g.
// "openrouter/qwen/qwen-2.5-72b-instruct" or "gpt-4o-mini") to the
// canonical upstream provider name used in suite_provider_health_log.
func providerForModel(model string) string {
	if model == "" {
		return ""
	}
	if i := strings.Index(model, "/"); i > 0 {
		return strings.ToLower(strings.TrimSpace(model[:i]))
	}
	switch {
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "gemini"):
		return "google"
	case strings.HasPrefix(model, "qwen"):
		return "qwen"
	case strings.HasPrefix(model, "o1"),
		strings.HasPrefix(model, "gpt"),
		strings.HasPrefix(model, "text-"),
		strings.HasPrefix(model, "dall-e"):
		return "openai"
	}
	return "other"
}

// perProviderStatus collapses one group's endpoint counts into the
// status string consumed by the dashboard ("healthy" / "degraded" /
// "unhealthy").
func perProviderStatus(g *providerEndpointGroup) string {
	if g == nil {
		return "unhealthy"
	}
	if g.unhealthy == 0 {
		return "healthy"
	}
	if g.healthy == 0 {
		return "unhealthy"
	}
	return "degraded"
}

func ReadProviderHealth(ctx context.Context, pool *pgxpool.Pool, window time.Duration) (ProviderHealthList, error) {
	if window <= 0 {
		window = 24 * time.Hour
	}
	rows, err := pool.Query(ctx, `
		with scoped as (
		  select provider, status, latency_ms, observed_at
		    from suite_provider_health_log
		   where observed_at >= now() - $1::interval
		), ranked as (
		  select provider,
		         count(*)::int as observations,
		         avg(case when status = 'healthy' then 1.0 else 0.0 end) * 100.0 as availability_pct,
		         percentile_cont(0.5) within group (order by latency_ms)::int as median_latency_ms,
		         percentile_cont(0.95) within group (order by latency_ms)::int as p95_latency_ms,
		         max(observed_at) as last_observed_at
		    from scoped
		   group by provider
		), latest as (
		  select distinct on (provider) provider, status
		    from scoped
		   order by provider, observed_at desc
		)
		select ranked.provider, latest.status, ranked.observations,
		       coalesce(ranked.availability_pct, 0), coalesce(ranked.median_latency_ms, 0),
		       coalesce(ranked.p95_latency_ms, 0), ranked.last_observed_at
		  from ranked
		  join latest using (provider)
		 order by ranked.provider
	`, intervalLiteral(window))
	if err != nil {
		return ProviderHealthList{}, err
	}
	defer rows.Close()
	out := ProviderHealthList{Providers: []ProviderHealthSnapshot{}, Window: window.String()}
	for rows.Next() {
		var (
			item ProviderHealthSnapshot
			last time.Time
		)
		if err := rows.Scan(&item.Provider, &item.Status, &item.Observations,
			&item.AvailabilityPct, &item.MedianLatencyMS, &item.P95LatencyMS, &last); err != nil {
			return ProviderHealthList{}, err
		}
		lastStr := last.UTC().Format(time.RFC3339Nano)
		item.LastObservedAt = &lastStr
		item.LatencyBuckets = map[string]int{}
		out.Providers = append(out.Providers, item)
	}
	return out, rows.Err()
}

func intervalLiteral(d time.Duration) string {
	seconds := int64(d.Seconds())
	if seconds < 60 {
		seconds = 60
	}
	return strconv.FormatInt(seconds, 10) + " seconds"
}
