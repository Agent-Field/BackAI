// metrics_summary.go — the at-a-glance metrics surface used by the
// dashboard's /operate/metrics tab.
//
// We do NOT try to replicate the full Prometheus surface here — operators
// looking for that open the side panel and talk to Prometheus directly.
// This handler aggregates a small ring buffer of recent request
// durations + a few `runtime` package metrics into the MetricsSummary
// shape declared in apps/dashboard/src/lib/api.ts.
//
// The ring buffer is owned by the HTTP middleware (see metrics_ring.go
// in this package). The summary handler is a pure reader.
package server

import (
	"net/http"
	"runtime"
	"sort"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// metricsSummaryResponse mirrors MetricsSummarySchema.
type metricsSummaryResponse struct {
	HTTPRequestsTotal int64                  `json:"http_requests_total"`
	HTTPP95MS         float64                `json:"http_p95_ms"`
	Goroutines        int                    `json:"goroutines"`
	HeapAllocBytes    uint64                 `json:"heap_alloc_bytes"`
	UptimeSeconds     int64                  `json:"uptime_seconds"`
	Version           string                 `json:"version"`
	ByRoute           []metricsByRouteEntry  `json:"by_route"`
}

// metricsByRouteEntry mirrors one row inside MetricsSummarySchema.by_route.
type metricsByRouteEntry struct {
	Route      string  `json:"route"`
	Requests   int64   `json:"requests"`
	AvgMS      float64 `json:"avg_ms"`
	ErrorCount int64   `json:"error_count"`
}

// registerMetricsRoutes mounts /api/v1/metrics/summary. /metrics
// continues to serve the Prometheus exposition format under the
// existing handler in server.go.
func (s *Server) registerMetricsRoutes() {
	s.mux.HandleFunc("GET /api/v1/metrics/summary", s.handleMetricsSummary)
}

// handleMetricsSummary returns the dashboard's at-a-glance view.
//
// Everything is computed lazily: the ring buffer is locked once to
// snapshot durations and per-route counters; runtime.ReadMemStats is
// a single global lock acquisition. No persistent state is touched.
func (s *Server) handleMetricsSummary(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.metrics.summary")
	defer span.End()

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	uptime := int64(0)
	if s.health != nil {
		uptime = int64(time.Since(s.health.StartedAt).Seconds())
	}

	ring := s.metricsRing
	var (
		total int64
		p95   float64
		rows  []metricsByRouteEntry
	)
	if ring != nil {
		total = ring.totalRequests()
		p95 = ring.p95()
		stats := ring.routeStats()
		rows = make([]metricsByRouteEntry, 0, len(stats))
		for _, st := range stats {
			avg := 0.0
			if st.count > 0 {
				avg = float64(st.totalMS) / float64(st.count)
			}
			rows = append(rows, metricsByRouteEntry{
				Route:      st.route,
				Requests:   st.count,
				AvgMS:      avg,
				ErrorCount: st.errors,
			})
		}
		// Top-10 by request count, descending. Ties broken by route name
		// so the dashboard order is deterministic.
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Requests != rows[j].Requests {
				return rows[i].Requests > rows[j].Requests
			}
			return rows[i].Route < rows[j].Route
		})
		if len(rows) > 10 {
			rows = rows[:10]
		}
	}
	if rows == nil {
		rows = []metricsByRouteEntry{}
	}

	writeJSON(w, http.StatusOK, metricsSummaryResponse{
		HTTPRequestsTotal: total,
		HTTPP95MS:         p95,
		Goroutines:        runtime.NumGoroutine(),
		HeapAllocBytes:    mem.HeapAlloc,
		UptimeSeconds:     uptime,
		Version:           s.runtimeVersion(),
		ByRoute:           rows,
	})
}

// runtimeVersion exposes a stable version string for the summary. The
// concrete value is whatever main.go writes via SetVersion at boot; if
// nothing has been set we fall back to "dev" so the dashboard never
// renders an empty pill.
func (s *Server) runtimeVersion() string {
	if s.version == "" {
		return "dev"
	}
	return s.version
}

// registerMetricsOpenAPI exposes /api/v1/metrics/summary in the spec.
func (s *Server) registerMetricsOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/metrics/summary", openapi.RouteMeta{
		Summary: "At-a-glance runtime metrics summary", Tags: []string{"system"},
	})
}
