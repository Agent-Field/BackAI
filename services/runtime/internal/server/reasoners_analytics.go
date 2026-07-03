// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type reasonerAnalyticsResponse struct {
	Reasoners []reasonerAnalyticsRow `json:"reasoners"`
	Window    analyticsWindow        `json:"window"`
}

type reasonerAnalyticsRow struct {
	Agent          string  `json:"agent"`
	Reasoner       string  `json:"reasoner"`
	Calls          int64   `json:"calls"`
	Errors         int64   `json:"errors"`
	ErrorRate      float64 `json:"error_rate"`
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
	CostUSD        float64 `json:"cost_usd"`
	LastCalledAt   string  `json:"last_called_at,omitempty"`
	TopCallerAgent string  `json:"top_caller_agent,omitempty"`
}

type analyticsWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (s *Server) handleReasonersAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.reasoners.analytics")
	defer span.End()

	from, to := parseAnalyticsWindow(r)
	limit := parseAnalyticsLimit(r.URL.Query().Get("limit"), 100)
	resp := reasonerAnalyticsResponse{
		Reasoners: []reasonerAnalyticsRow{},
		Window: analyticsWindow{
			From: from.UTC().Format(time.RFC3339Nano),
			To:   to.UTC().Format(time.RFC3339Nano),
		},
	}
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := s.queryReasonerAnalytics(ctx, from, to, limit)
	if err != nil {
		s.log.Error("reasoner analytics failed", "error", err)
		span.RecordError(err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	resp.Reasoners = rows
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) queryReasonerAnalytics(ctx context.Context, from, to time.Time, limit int) ([]reasonerAnalyticsRow, error) {
	// Operator cross-tenant aggregate over the RLS-protected
	// suite_cost_events table — same bypass pattern as queryRuns
	// (dashboard.go). The route is operator-gated in server.go.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `set local app.bypass_rls = 'on'`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
        select
          coalesce(agent, 'none') as agent,
          coalesce(reasoner, 'none') as reasoner,
          count(*)::bigint as calls,
          count(*) filter (
            where coalesce(status_code, 200) < 200
               or coalesce(status_code, 200) >= 400
               or error_code is not null
          )::bigint as errors,
          coalesce(avg(latency_ms), 0)::float8 as avg_latency_ms,
          coalesce(sum(cost_usd), 0)::float8 as cost_usd,
          max(occurred_at) as last_called_at
        from suite_cost_events
        where occurred_at >= $1
          and occurred_at < $2
          and nullif(reasoner, '') is not null
        group by coalesce(agent, 'none'), coalesce(reasoner, 'none')
        order by cost_usd desc, calls desc, reasoner asc
        limit $3
    `, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []reasonerAnalyticsRow{}
	for rows.Next() {
		var row reasonerAnalyticsRow
		var last time.Time
		if err := rows.Scan(
			&row.Agent,
			&row.Reasoner,
			&row.Calls,
			&row.Errors,
			&row.AvgLatencyMS,
			&row.CostUSD,
			&last,
		); err != nil {
			return nil, err
		}
		if row.Calls > 0 {
			row.ErrorRate = float64(row.Errors) / float64(row.Calls)
		}
		row.LastCalledAt = last.UTC().Format(time.RFC3339Nano)
		row.TopCallerAgent = row.Agent
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

func parseAnalyticsWindow(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now
	q := r.URL.Query()
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			from = parsed.UTC()
		}
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			to = parsed.UTC()
		}
	}
	if !from.Before(to) {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}

func parseAnalyticsLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	if n > 500 {
		return 500
	}
	return n
}
