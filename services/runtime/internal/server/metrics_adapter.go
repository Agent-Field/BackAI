// SPDX-License-Identifier: Apache-2.0

// metrics_adapter.go — admin metrics adapter REST surface.
package server

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type metricsInstantResponse struct {
	Samples []metricsInstantWire `json:"samples"`
	Total   int                  `json:"total"`
}

type metricsRangeResponse struct {
	Series []metricsRangeWire `json:"series"`
	Total  int                `json:"total"`
}

type metricsInstantWire struct {
	Metric map[string]string `json:"metric"`
	Value  float64           `json:"value"`
	TS     string            `json:"ts"`
}

type metricsRangeWire struct {
	Metric map[string]string       `json:"metric"`
	Values []metricsTimedValueWire `json:"values"`
}

type metricsTimedValueWire struct {
	TS    string  `json:"ts"`
	Value float64 `json:"value"`
}

func (s *Server) registerAdminMetricsRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/metrics/query", s.handleAdminMetricsQuery)
	s.mux.HandleFunc("GET /api/v1/admin/metrics/range", s.handleAdminMetricsRange)
	s.mux.HandleFunc("GET /api/v1/admin/metrics/capabilities", s.handleAdminMetricsCapabilities)
}

func (s *Server) handleAdminMetricsQuery(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.metrics.query")
	defer span.End()
	if s.metricsStore == nil {
		writeMetricsStoreError(w, metrics.ErrNoBackend)
		return
	}
	promQL := strings.TrimSpace(r.URL.Query().Get("promql"))
	if promQL == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "promql is required", nil)
		return
	}
	at, err := parseOptionalMetricTime(r.URL.Query().Get("at"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "at must be RFC3339 or Unix seconds", nil)
		return
	}
	samples, err := s.metricsStore.Query(r.Context(), promQL, at)
	if err != nil {
		writeMetricsStoreError(w, err)
		return
	}
	out := metricsInstantToWire(samples)
	writeJSON(w, http.StatusOK, metricsInstantResponse{Samples: out, Total: len(out)})
}

func (s *Server) handleAdminMetricsRange(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.metrics.range")
	defer span.End()
	if s.metricsStore == nil {
		writeMetricsStoreError(w, metrics.ErrNoBackend)
		return
	}
	q := r.URL.Query()
	promQL := strings.TrimSpace(q.Get("promql"))
	if promQL == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "promql is required", nil)
		return
	}
	from, err := parseRequiredMetricTime(q.Get("from"), "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	to, err := parseRequiredMetricTime(q.Get("to"), "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
		return
	}
	if !to.After(from) {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "to must be after from", nil)
		return
	}
	step, err := time.ParseDuration(defaultString(strings.TrimSpace(q.Get("step")), "5m"))
	if err != nil || step <= 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "step must be a positive duration", nil)
		return
	}
	series, err := s.metricsStore.QueryRange(r.Context(), promQL, from, to, step)
	if err != nil {
		writeMetricsStoreError(w, err)
		return
	}
	out := metricsRangeToWire(series)
	writeJSON(w, http.StatusOK, metricsRangeResponse{Series: out, Total: len(out)})
}

func (s *Server) handleAdminMetricsCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.metricsStore == nil {
		writeJSON(w, http.StatusOK, metrics.Capabilities{})
		return
	}
	writeJSON(w, http.StatusOK, s.metricsStore.Capabilities())
}

func writeMetricsStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metrics.ErrNoBackend):
		writeError(w, http.StatusServiceUnavailable, "metrics_no_backend", "no metrics backend configured", nil)
	case errors.Is(err, metrics.ErrUnsupportedShape):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_result_shape", err.Error(), nil)
	case errors.Is(err, metrics.ErrUnsupportedBackend):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_metrics_backend", err.Error(), nil)
	default:
		writeError(w, http.StatusBadGateway, "METRICS_QUERY_FAILED", err.Error(), nil)
	}
}

func parseOptionalMetricTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	return parseMetricTime(raw)
}

func parseRequiredMetricTime(raw, name string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New(name + " is required")
	}
	ts, err := parseMetricTime(raw)
	if err != nil {
		return time.Time{}, errors.New(name + " must be RFC3339 or Unix seconds")
	}
	return ts, nil
}

func parseMetricTime(raw string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC(), nil
	}
	sec, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, err
	}
	i, frac := math.Modf(sec)
	return time.Unix(int64(i), int64(frac*1e9)).UTC(), nil
}

func metricsInstantToWire(samples []metrics.InstantSample) []metricsInstantWire {
	out := make([]metricsInstantWire, 0, len(samples))
	for _, sample := range samples {
		out = append(out, metricsInstantWire{
			Metric: sample.Metric,
			Value:  sample.Value,
			TS:     timeToWire(sample.TS),
		})
	}
	return out
}

func metricsRangeToWire(series []metrics.RangeSample) []metricsRangeWire {
	out := make([]metricsRangeWire, 0, len(series))
	for _, item := range series {
		values := make([]metricsTimedValueWire, 0, len(item.Values))
		for _, value := range item.Values {
			values = append(values, metricsTimedValueWire{TS: timeToWire(value.TS), Value: value.Value})
		}
		out = append(out, metricsRangeWire{Metric: item.Metric, Values: values})
	}
	return out
}

func (s *Server) registerAdminMetricsOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/metrics/query", openapi.RouteMeta{
		Summary: "Run an instant query through the active metrics adapter",
		Tags:    []string{"admin"},
		Parameters: []openapi.Parameter{
			{Name: "promql", In: "query", Required: true, Schema: map[string]any{"type": "string"}},
			{Name: "at", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
	s.openapi.Register("GET", "/api/v1/admin/metrics/range", openapi.RouteMeta{
		Summary: "Run a range query through the active metrics adapter",
		Tags:    []string{"admin"},
		Parameters: []openapi.Parameter{
			{Name: "promql", In: "query", Required: true, Schema: map[string]any{"type": "string"}},
			{Name: "from", In: "query", Required: true, Schema: map[string]any{"type": "string"}},
			{Name: "to", In: "query", Required: true, Schema: map[string]any{"type": "string"}},
			{Name: "step", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
	s.openapi.Register("GET", "/api/v1/admin/metrics/capabilities", openapi.RouteMeta{
		Summary: "Get active metrics adapter capabilities", Tags: []string{"admin"},
	})
}
