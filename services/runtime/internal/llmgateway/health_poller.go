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
	status := "healthy"
	details := map[string]any{}
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
		status = "unhealthy"
		details["error"] = err.Error()
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode == http.StatusUnauthorized {
			p.log.Warn("provider health poll skipped; LiteLLM rejected master key",
				"status_code", resp.StatusCode)
			return
		}
		if resp.StatusCode >= 500 {
			status = "unhealthy"
		} else if resp.StatusCode >= 300 {
			status = "degraded"
		}
		var parsed map[string]any
		if json.Unmarshal(body, &parsed) == nil {
			details = parsed
		} else if len(body) > 0 {
			details["body"] = string(body)
		}
		details["status_code"] = resp.StatusCode
	}
	detailsJSON, _ := json.Marshal(details)
	if _, err := p.pool.Exec(ctx, `
		insert into suite_provider_health_log (provider, status, latency_ms, details)
		values ($1, $2, $3, $4::jsonb)
	`, "litellm", status, latency, detailsJSON); err != nil {
		p.log.Warn("provider health insert failed", "error", err)
	}
	p.prune(ctx)
}

func (p *ProviderHealthPoller) prune(ctx context.Context) {
	if _, err := p.pool.Exec(ctx, `
		delete from suite_provider_health_log
		 where observed_at < now() - interval '30 days'
	`); err != nil {
		p.log.Warn("provider health prune failed", "error", err)
	}
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
