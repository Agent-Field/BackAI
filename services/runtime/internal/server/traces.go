// SPDX-License-Identifier: Apache-2.0

// traces.go — store-backed trace search/detail REST surface.
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type traceSummaryWire struct {
	TraceID       string `json:"trace_id"`
	RootService   string `json:"root_service"`
	RootOperation string `json:"root_operation"`
	StartTime     string `json:"start_time"`
	DurationMs    int64  `json:"duration_ms"`
	SpanCount     int    `json:"span_count"`
	Status        string `json:"status"`
}

type spanWire struct {
	SpanID       string          `json:"span_id"`
	ParentSpanID string          `json:"parent_span_id"`
	Service      string          `json:"service"`
	Operation    string          `json:"operation"`
	StartTime    string          `json:"start_time"`
	DurationMs   int64           `json:"duration_ms"`
	Status       string          `json:"status"`
	Attributes   map[string]any  `json:"attributes,omitempty"`
	Events       []spanEventWire `json:"events,omitempty"`
}

type spanEventWire struct {
	TS         string         `json:"ts"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type traceSearchResponse struct {
	Traces  []traceSummaryWire `json:"traces"`
	Total   int                `json:"total"`
	HasMore bool               `json:"has_more,omitempty"`
}

type traceDetailResponse struct {
	TraceID string     `json:"trace_id"`
	Spans   []spanWire `json:"spans"`
}

func (s *Server) registerTraceRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/traces", s.handleAdminSearchTraces)
	s.mux.HandleFunc("GET /api/v1/admin/traces/{trace_id}", s.handleAdminGetTrace)
	s.mux.HandleFunc("GET /api/v1/admin/traces/capabilities", s.handleAdminTraceCapabilities)
}

func (s *Server) handleAdminSearchTraces(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.traces.search")
	defer span.End()
	if s.tracesStore == nil {
		writeJSON(w, http.StatusOK, traceSearchResponse{Traces: []traceSummaryWire{}})
		return
	}
	filter, err := traceFilterFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	result, err := s.tracesStore.Search(r.Context(), filter)
	if err != nil {
		writeTraceStoreError(w, err)
		return
	}
	out := traceSummariesToWire(result.Traces)
	writeJSON(w, http.StatusOK, traceSearchResponse{Traces: out, Total: len(out), HasMore: result.HasMore})
}

func (s *Server) handleAdminGetTrace(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.traces.get")
	defer span.End()
	if s.tracesStore == nil {
		writeError(w, http.StatusServiceUnavailable, "traces_no_backend", "traces adapter is not configured", nil)
		return
	}
	traceID := strings.TrimSpace(r.PathValue("trace_id"))
	trace, err := s.tracesStore.Get(r.Context(), traceID)
	if err != nil {
		writeTraceStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, traceToWire(trace))
}

func (s *Server) handleAdminTraceCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.tracesStore == nil {
		writeJSON(w, http.StatusOK, traces.Capabilities{})
		return
	}
	writeJSON(w, http.StatusOK, s.tracesStore.Capabilities())
}

func writeTraceStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, traces.ErrInvalidTraceID):
		writeError(w, http.StatusBadRequest, "invalid_trace_id", err.Error(), nil)
	case errors.Is(err, traces.ErrTraceNotFound):
		writeError(w, http.StatusNotFound, "trace_not_found", "trace not found", nil)
	case errors.Is(err, traces.ErrNoBackend):
		writeError(w, http.StatusServiceUnavailable, "traces_no_backend", "no traces backend configured", nil)
	default:
		writeError(w, http.StatusBadGateway, "TRACES_QUERY_FAILED", err.Error(), nil)
	}
}

func traceFilterFromRequest(r *http.Request) (traces.SearchFilter, error) {
	q := r.URL.Query()
	filter := traces.SearchFilter{
		Service:   strings.TrimSpace(q.Get("service")),
		Operation: strings.TrimSpace(q.Get("operation")),
		Status:    strings.TrimSpace(q.Get("status")),
		Tag:       traceTagsFromQuery(qValues(q, "tag", "tags")),
	}
	if limitRaw := q.Get("limit"); limitRaw != "" {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil || limit < 0 {
			return traces.SearchFilter{}, errors.New("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	if fromRaw := q.Get("from"); fromRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, fromRaw)
		if err != nil {
			return traces.SearchFilter{}, errors.New("from must be RFC3339")
		}
		filter.From = ts
	}
	if toRaw := q.Get("to"); toRaw != "" {
		ts, err := time.Parse(time.RFC3339Nano, toRaw)
		if err != nil {
			return traces.SearchFilter{}, errors.New("to must be RFC3339")
		}
		filter.To = ts
	}
	if raw := firstQuery(q, "duration_gt", "min_duration"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return traces.SearchFilter{}, errors.New("duration_gt must be a duration")
		}
		filter.MinDuration = d
	}
	if raw := firstQuery(q, "duration_lt", "max_duration"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return traces.SearchFilter{}, errors.New("duration_lt must be a duration")
		}
		filter.MaxDuration = d
	}
	return filter, nil
}

func traceTagsFromQuery(values []string) map[string]string {
	out := map[string]string{}
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key, value, ok := strings.Cut(part, "=")
			if ok && strings.TrimSpace(key) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func traceSummariesToWire(in []traces.TraceSummary) []traceSummaryWire {
	out := make([]traceSummaryWire, 0, len(in))
	for _, item := range in {
		out = append(out, traceSummaryToWire(item))
	}
	return out
}

func traceSummaryToWire(item traces.TraceSummary) traceSummaryWire {
	return traceSummaryWire{
		TraceID:       item.TraceID,
		RootService:   defaultString(item.RootService, "unknown"),
		RootOperation: defaultString(item.RootOperation, "trace"),
		StartTime:     timeToWire(item.StartTime),
		DurationMs:    item.Duration.Milliseconds(),
		SpanCount:     item.SpanCount,
		Status:        traceStatus(item.Status),
	}
}

func traceToWire(trace traces.Trace) traceDetailResponse {
	spans := make([]spanWire, 0, len(trace.Spans))
	for _, span := range trace.Spans {
		events := make([]spanEventWire, 0, len(span.Events))
		for _, event := range span.Events {
			events = append(events, spanEventWire{
				TS:         timeToWire(event.TS),
				Name:       event.Name,
				Attributes: event.Attributes,
			})
		}
		spans = append(spans, spanWire{
			SpanID:       span.SpanID,
			ParentSpanID: span.ParentSpanID,
			Service:      defaultString(span.Service, "unknown"),
			Operation:    defaultString(span.Operation, "span"),
			StartTime:    timeToWire(span.StartTime),
			DurationMs:   span.Duration.Milliseconds(),
			Status:       traceStatus(span.Status),
			Attributes:   span.Attributes,
			Events:       events,
		})
	}
	return traceDetailResponse{TraceID: trace.TraceID, Spans: spans}
}

func traceStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "error", "unset":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "unset"
	}
}

func timeToWire(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func (s *Server) registerTraceOpenAPI() {
	params := []openapi.Parameter{
		{Name: "service", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "operation", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "tag", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "status", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "from", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
		{Name: "to", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
		{Name: "duration_gt", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "duration_lt", In: "query", Schema: map[string]any{"type": "string"}},
		{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
	}
	s.openapi.Register("GET", "/api/v1/admin/traces", openapi.RouteMeta{
		Summary: "Search traces through the active traces adapter", Tags: []string{"admin"}, Parameters: params,
	})
	s.openapi.Register("GET", "/api/v1/admin/traces/{trace_id}", openapi.RouteMeta{
		Summary: "Get a trace through the active traces adapter", Tags: []string{"admin"},
		Parameters: []openapi.Parameter{{Name: "trace_id", In: "path", Required: true, Schema: map[string]any{"type": "string"}}},
	})
	s.openapi.Register("GET", "/api/v1/admin/traces/capabilities", openapi.RouteMeta{
		Summary: "Get active traces adapter capabilities", Tags: []string{"admin"},
	})
}
