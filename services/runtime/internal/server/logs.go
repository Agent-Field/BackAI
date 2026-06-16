// SPDX-License-Identifier: Apache-2.0

// logs.go — store-backed logs REST/SSE surface.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type logLineWire struct {
	TS        string         `json:"ts"`
	Level     string         `json:"level"`
	Service   string         `json:"service"`
	Msg       string         `json:"msg"`
	RequestID string         `json:"request_id,omitempty"`
	TenantID  string         `json:"tenant_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
	Agent     string         `json:"agent,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type logsResponse struct {
	Logs       []logLineWire `json:"logs"`
	Lines      []logLineWire `json:"lines,omitempty"`
	Total      int           `json:"total,omitempty"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more,omitempty"`
}

// registerLogsRoutes wires both the historical /api/v1/logs endpoint and the
// Block 3 admin logs adapter endpoints.
func (s *Server) registerLogsRoutes() {
	s.mux.HandleFunc("GET /api/v1/logs", s.handleListLogsCompat)
	s.mux.HandleFunc("GET /api/v1/admin/logs", s.handleAdminListLogs)
	s.mux.HandleFunc("GET /api/v1/admin/logs/tail", s.handleAdminTailLogs)
	s.mux.HandleFunc("GET /api/v1/admin/logs/capabilities", s.handleAdminLogCapabilities)
}

func (s *Server) handleListLogsCompat(w http.ResponseWriter, r *http.Request) {
	resp, ok := s.queryLogs(w, r)
	if !ok {
		return
	}
	resp.Lines = resp.Logs
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminListLogs(w http.ResponseWriter, r *http.Request) {
	resp, ok := s.queryLogs(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) queryLogs(w http.ResponseWriter, r *http.Request) (logsResponse, bool) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.logs.list")
	defer span.End()
	if s.logsStore == nil {
		return logsResponse{Logs: []logLineWire{}}, true
	}
	filter, err := filterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return logsResponse{}, false
	}
	page, err := s.logsStore.Query(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusBadGateway, "LOGS_QUERY_FAILED", err.Error(), nil)
		return logsResponse{}, false
	}
	out := entriesToWire(page.Entries)
	return logsResponse{
		Logs:       out,
		Total:      len(out),
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	}, true
}

func (s *Server) handleAdminTailLogs(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.logs.tail")
	defer span.End()
	if s.logsStore == nil {
		writeError(w, http.StatusServiceUnavailable, "LOGS_NOT_CONFIGURED", "logs adapter is not configured", nil)
		return
	}
	caps := s.logsStore.Capabilities()
	if !caps.SupportsTail {
		writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", "active logs adapter does not support tail", nil)
		return
	}
	filter, err := filterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	ch, err := s.logsStore.Tail(r.Context(), filter)
	if err != nil {
		if errors.Is(err, logs.ErrUnsupportedCapability) {
			writeError(w, http.StatusUnprocessableEntity, "unsupported_capability", err.Error(), nil)
			return
		}
		writeError(w, http.StatusBadGateway, "LOGS_TAIL_FAILED", err.Error(), nil)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "response writer does not support streaming", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-ch:
			if !ok {
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				return
			}
			_, _ = w.Write([]byte("data: "))
			if err := enc.Encode(entryToWire(entry)); err != nil {
				return
			}
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
	}
}

func (s *Server) handleAdminLogCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.logsStore == nil {
		writeJSON(w, http.StatusOK, logs.Capabilities{})
		return
	}
	writeJSON(w, http.StatusOK, s.logsStore.Capabilities())
}

func filterFromRequest(r *http.Request) (logs.Filter, error) {
	q := r.URL.Query()
	filter := logs.Filter{
		Services:      splitQuery(qValues(q, "service", "services")),
		Levels:        splitQuery(qValues(q, "level", "levels")),
		TenantID:      firstQuery(q, "tenant", "tenant_id"),
		RequestID:     firstQuery(q, "request_id", "request"),
		TraceID:       firstQuery(q, "trace_id", "trace"),
		Search:        q.Get("search"),
		SearchIsRegex: parseBool(q.Get("search_is_regex")),
		Cursor:        q.Get("cursor"),
	}
	if limitRaw := q.Get("limit"); limitRaw != "" {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil || limit < 0 {
			return logs.Filter{}, errors.New("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	if fromRaw := q.Get("from"); fromRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, fromRaw)
		if err != nil {
			return logs.Filter{}, errors.New("from must be RFC3339")
		}
		filter.From = ts
	}
	if toRaw := q.Get("to"); toRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, toRaw)
		if err != nil {
			return logs.Filter{}, errors.New("to must be RFC3339")
		}
		filter.To = ts
	}
	return filter, nil
}

func entriesToWire(entries []logs.Entry) []logLineWire {
	out := make([]logLineWire, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entryToWire(entry))
	}
	return out
}

func entryToWire(entry logs.Entry) logLineWire {
	level := strings.ToLower(strings.TrimSpace(entry.Level))
	switch level {
	case "debug", "info", "warn", "error":
	default:
		if level == "fatal" {
			level = "error"
		} else {
			level = "info"
		}
	}
	ts := entry.TS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return logLineWire{
		TS:        ts.UTC().Format(time.RFC3339Nano),
		Level:     level,
		Service:   defaultString(entry.Service, "runtime"),
		Msg:       entry.Msg,
		RequestID: entry.RequestID,
		TenantID:  entry.TenantID,
		TraceID:   entry.TraceID,
		Agent:     entry.Agent,
		Fields:    entry.Fields,
	}
}

func splitQuery(values []string) []string {
	var out []string
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func qValues(q map[string][]string, names ...string) []string {
	var out []string
	for _, name := range names {
		out = append(out, q[name]...)
	}
	return out
}

func firstQuery(q map[string][]string, names ...string) string {
	for _, name := range names {
		if values := q[name]; len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
	}
	return ""
}

func parseBool(raw string) bool {
	return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (s *Server) registerLogsOpenAPI() {
	b := s.openapi
	params := []openapi.Parameter{
		{Name: "service", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "level", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "request_id", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "trace_id", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "search", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "search_is_regex", In: "query", Schema: map[string]any{"type": "boolean"}},
		{Name: "from", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
		{Name: "to", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
		{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
		{Name: "cursor", In: "query", Schema: map[string]any{"type": "string"}},
	}
	b.Register("GET", "/api/v1/logs", openapi.RouteMeta{
		Summary: "Compatibility recent log lines", Tags: []string{"system"}, Parameters: params,
	})
	b.Register("GET", "/api/v1/admin/logs", openapi.RouteMeta{
		Summary: "Query log lines through the active logs adapter", Tags: []string{"admin"}, Parameters: params,
	})
	b.Register("GET", "/api/v1/admin/logs/tail", openapi.RouteMeta{
		Summary: "Tail log lines through the active logs adapter", Tags: []string{"admin"}, Parameters: params,
	})
	b.Register("GET", "/api/v1/admin/logs/capabilities", openapi.RouteMeta{
		Summary: "Get active logs adapter capabilities", Tags: []string{"admin"},
	})
}
