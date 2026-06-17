// SPDX-License-Identifier: Apache-2.0

// Unified top-bar anchors endpoint.
//
// Closes Gap 6 from development/ux/required-backend-gaps.md. The
// dashboard's top bar shows three live values on every page (Inbox /
// Cost / Health). Surfacing them through a single endpoint means each
// page navigation costs one fetch instead of three.
//
// Shape matches AdminAnchorsSchema in apps/dashboard/src/lib/api.ts.
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/approvals"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type adminAnchorsResponse struct {
	InboxPending int     `json:"inbox_pending"`
	CostTodayUSD float64 `json:"cost_today_usd"`
	// Health: "healthy" when both AF and DB respond; "degraded" when one
	// is failing; "down" when both fail. Mirrors the alerts logic in
	// handleHomeOverview so the top-bar dot and the KPI strip never disagree.
	Health string `json:"health"`
}

func (s *Server) registerAdminAnchorsRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/anchors", s.handleAdminAnchors)
}

func (s *Server) registerAdminAnchorsOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/anchors", openapi.RouteMeta{
		Summary: "Unified top-bar anchor values (Inbox count, Cost today, Health)",
		Tags:    []string{"admin"},
	})
}

func (s *Server) handleAdminAnchors(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.admin.anchors")
	defer span.End()

	resp := adminAnchorsResponse{Health: "healthy"}

	// Inbox: pending approvals across all tenants. Other inbox-able items
	// (budget alerts, error spikes) ship with Gap 9 — until then approvals
	// are the only signal.
	if s.approvals != nil {
		list, err := s.approvals.List(ctx, approvals.ListInput{
			Status: "pending",
			Limit:  1,
		})
		if err == nil {
			resp.InboxPending = list.Total
		} else {
			s.log.Warn("anchors: approvals query failed", "error", err)
			span.RecordError(err)
		}
	}

	// Cost today: same query as handleHomeOverview so the anchor and the
	// KPI strip always agree.
	if s.db != nil && s.db.Pool != nil {
		now := time.Now().UTC()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		var costToday float64
		err := s.db.Pool.QueryRow(ctx,
			"select coalesce(sum(cost_usd), 0) from suite_cost_events where occurred_at >= $1",
			todayStart).Scan(&costToday)
		if err != nil {
			s.log.Warn("anchors: cost today query failed", "error", err)
			span.RecordError(err)
		}
		resp.CostTodayUSD = costToday
	}

	// Health: probe AF + DB with the same short timeout as the alerts
	// section. Both healthy -> healthy. Mixed -> degraded. Both failed -> down.
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	afOK := true
	dbOK := true
	if s.af != nil {
		if _, err := s.af.Health(probeCtx); err != nil {
			afOK = false
		}
	}
	if s.db != nil {
		if err := s.db.Health(probeCtx); err != nil {
			dbOK = false
		}
	}
	switch {
	case afOK && dbOK:
		resp.Health = "healthy"
	case !afOK && !dbOK:
		resp.Health = "down"
	default:
		resp.Health = "degraded"
	}

	writeJSON(w, http.StatusOK, resp)
}
