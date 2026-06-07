// logs.go — recent-log-lines REST surface.
//
// Backed by the in-memory logger.Ring the runtime owns. Single-process
// only; multi-process deployments should run a real log aggregator
// (Loki / Vector / etc) and disable this endpoint.
//
// Wire shape matches LogLineSchema in apps/dashboard/src/lib/api.ts:
// the `fields` map carries everything that didn't fit into a
// dashboard-known column.
package server

import (
	"net/http"
	"strconv"

	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type logLineWire struct {
	TS        string         `json:"ts"`
	Level     string         `json:"level"`
	Service   string         `json:"service"`
	Msg       string         `json:"msg"`
	RequestID string         `json:"request_id,omitempty"`
	TenantID  string         `json:"tenant_id,omitempty"`
	Agent     string         `json:"agent,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type logsResponse struct {
	Lines []logLineWire `json:"lines"`
}

// registerLogsRoutes wires /api/v1/logs.
func (s *Server) registerLogsRoutes() {
	s.mux.HandleFunc("GET /api/v1/logs", s.handleListLogs)
}

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.logs.list")
	defer span.End()

	if s.logRing == nil {
		writeJSON(w, http.StatusOK, logsResponse{Lines: []logLineWire{}})
		return
	}

	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			if n > 0 && n <= 1000 {
				limit = n
			}
		}
	}
	lines := s.logRing.Recent(limit)
	out := make([]logLineWire, 0, len(lines))
	for _, l := range lines {
		out = append(out, logLineWire{
			TS:        l.TS,
			Level:     string(coerceLineLevel(l.Level)),
			Service:   l.Service,
			Msg:       l.Msg,
			RequestID: l.RequestID,
			TenantID:  l.TenantID,
			Agent:     l.Agent,
			Fields:    l.Fields,
		})
	}
	writeJSON(w, http.StatusOK, logsResponse{Lines: out})
}

// coerceLineLevel clamps a ring level into the dashboard's known set.
// Any unknown value (we currently don't produce one, but guard anyway)
// collapses to "info" so the zod schema never rejects the payload.
func coerceLineLevel(l logger.LineLevel) logger.LineLevel {
	switch l {
	case logger.LevelDebug, logger.LevelInfo, logger.LevelWarn, logger.LevelError:
		return l
	default:
		return logger.LevelInfo
	}
}

func (s *Server) registerLogsOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/logs", openapi.RouteMeta{
		Summary: "Recent log lines from the in-memory ring", Tags: []string{"system"},
		Parameters: []openapi.Parameter{
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
}
