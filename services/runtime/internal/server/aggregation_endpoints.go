// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type toolsUsageResponse struct {
	Tools  []toolUsageRow  `json:"tools"`
	Window analyticsWindow `json:"window"`
}

type toolUsageRow struct {
	ToolName       string  `json:"tool_name"`
	Transport      string  `json:"transport"`
	Calls          int64   `json:"calls"`
	Errors         int64   `json:"errors"`
	ErrorRate      float64 `json:"error_rate"`
	AvgDurationMS  float64 `json:"avg_duration_ms"`
	TopCallerAgent string  `json:"top_caller_agent,omitempty"`
	LastCalledAt   string  `json:"last_called_at,omitempty"`
}

type oauthRefreshHistoryResponse struct {
	Events []oauthRefreshHistoryEvent `json:"events"`
}

type oauthRefreshHistoryEvent struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	Provider    string  `json:"provider"`
	UserID      *string `json:"user_id"`
	Status      string  `json:"status"`
	ErrorCode   *string `json:"error_code"`
	AttemptedAt string  `json:"attempted_at"`
}

func (s *Server) handleToolsUsage(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.tools.usage")
	defer span.End()
	from, to := parseAnalyticsWindow(r)
	limit := parseAnalyticsLimit(r.URL.Query().Get("limit"), 100)
	resp := toolsUsageResponse{
		Tools: []toolUsageRow{},
		Window: analyticsWindow{
			From: from.UTC().Format(time.RFC3339Nano),
			To:   to.UTC().Format(time.RFC3339Nano),
		},
	}
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		s.log.Error("tools usage begin failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		s.log.Error("tools usage bypass failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	rows, err := tx.Query(ctx, `
        select
          tool_name,
          transport,
          count(*)::bigint as calls,
          count(*) filter (where status <> 'success')::bigint as errors,
          coalesce(avg(duration_ms), 0)::float8 as avg_duration_ms,
          mode() within group (order by agent_id) as top_caller_agent,
          max(called_at) as last_called_at
        from suite_tool_calls
        where called_at >= $1 and called_at < $2
        group by tool_name, transport
        order by calls desc, tool_name asc
        limit $3
    `, from, to, limit)
	if err != nil {
		s.log.Error("tools usage failed", "error", err)
		span.RecordError(err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row toolUsageRow
		var last time.Time
		if err := rows.Scan(&row.ToolName, &row.Transport, &row.Calls, &row.Errors, &row.AvgDurationMS, &row.TopCallerAgent, &last); err != nil {
			s.log.Error("tools usage scan failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
			return
		}
		if row.Calls > 0 {
			row.ErrorRate = float64(row.Errors) / float64(row.Calls)
		}
		row.LastCalledAt = last.UTC().Format(time.RFC3339Nano)
		resp.Tools = append(resp.Tools, row)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("tools usage rows failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.log.Error("tools usage commit failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleOAuthRefreshHistory(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.oauth.refresh_history")
	defer span.End()
	resp := oauthRefreshHistoryResponse{Events: []oauthRefreshHistoryEvent{}}
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	q := r.URL.Query()
	provider := strings.TrimSpace(q.Get("provider"))
	tenantID := strings.TrimSpace(q.Get("tenant_id"))
	limit := parseAnalyticsLimit(q.Get("limit"), 50)
	where := []string{"true"}
	args := []any{}
	if provider != "" {
		args = append(args, provider)
		where = append(where, "provider = $"+strconv.Itoa(len(args)))
	}
	if tenantID != "" {
		args = append(args, tenantID)
		where = append(where, "tenant_id = $"+strconv.Itoa(len(args)))
	}
	args = append(args, limit)
	sql := `
        select id::text, tenant_id::text, provider, user_id::text, status, error_code, attempted_at
        from suite_oauth_refresh_log
        where ` + strings.Join(where, " and ") + `
        order by attempted_at desc
        limit $` + strconv.Itoa(len(args))
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		s.log.Error("oauth refresh history begin failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		s.log.Error("oauth refresh history bypass failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		s.log.Error("oauth refresh history failed", "error", err)
		span.RecordError(err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row oauthRefreshHistoryEvent
		var attempted time.Time
		if err := rows.Scan(&row.ID, &row.TenantID, &row.Provider, &row.UserID, &row.Status, &row.ErrorCode, &attempted); err != nil {
			s.log.Error("oauth refresh history scan failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
			return
		}
		row.AttemptedAt = attempted.UTC().Format(time.RFC3339Nano)
		resp.Events = append(resp.Events, row)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("oauth refresh history rows failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.log.Error("oauth refresh history commit failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
