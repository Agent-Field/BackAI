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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type adminAnchorsResponse struct {
	// InboxPending counts every item that would land in /inbox: pending HITL
	// approvals + active system alerts. The top-bar badge renders this
	// directly, so the badge value matches what the operator sees on Inbox.
	InboxPending int `json:"inbox_pending"`
	// InboxHasCritical flips the badge to its critical (red) variant when
	// at least one inbox item is critical-severity. Today the critical
	// signal comes from the AgentField/DB unhealthy probes — approvals are
	// "watch", not "act".
	InboxHasCritical bool    `json:"inbox_has_critical"`
	CostTodayUSD     float64 `json:"cost_today_usd"`
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

	// Inbox count: pending approvals across all tenants + active system
	// alerts (added below once we know the probe outcomes). The approvals
	// store is tenant-scoped, so we query the table directly with
	// app.bypass_rls=on to get the true cross-tenant total — same pattern
	// other admin aggregations use (see aggregation_endpoints.go). Other
	// emitter classes — budget thresholds, error spikes, queue
	// backpressure — arrive with Gap 9.
	approvalsTotal := 0
	if s.db != nil && s.db.Pool != nil {
		count, err := countPendingApprovalsCrossTenant(ctx, s.db.Pool)
		if err == nil {
			approvalsTotal = count
		} else {
			s.log.Warn("anchors: pending approvals count failed", "error", err)
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

	// System alerts in the inbox tally. Mirrors the alerts emitted by
	// handleHomeOverview so the badge and the Inbox page agree on count.
	systemAlerts := 0
	if !afOK {
		systemAlerts++
	}
	if !dbOK {
		systemAlerts++
	}
	resp.InboxPending = approvalsTotal + systemAlerts
	resp.InboxHasCritical = systemAlerts > 0

	writeJSON(w, http.StatusOK, resp)
}

// countPendingApprovalsCrossTenant returns the total number of pending
// approval rows across every tenant. RLS would otherwise restrict the
// query to whatever app.tenant_id is set on the connection (empty here);
// we open a short transaction and flip app.bypass_rls so the admin badge
// reflects the platform-wide count.
func countPendingApprovalsCrossTenant(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return 0, err
	}
	var n int
	err = tx.QueryRow(ctx,
		"select count(*) from suite_approvals where status = 'pending'",
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
