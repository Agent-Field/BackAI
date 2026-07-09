// SPDX-License-Identifier: Apache-2.0

// Unified admin events feed.
//
// Closes Gap 5 from development/ux/required-backend-gaps.md. Merges four
// chronologically-distinct sources — agent runs, webhook deliveries, system
// alerts, and the customer-facing activity log — into a single typed list
// the dashboard can render as its Home activity feed without having to
// fan-out to multiple endpoints + merge client-side.
//
// Shape matches AdminEventSchema / AdminEventListSchema in
// apps/dashboard/src/lib/api.ts.
//
// Notes for future readers:
//   - This is a read-only aggregator. No persistence layer; every request
//     recomputes from the underlying tables / runtime objects.
//   - Sources that aren't wired (no s.activity, no s.db) are skipped
//     silently — the response still validates as a list, just shorter.
//   - The "alerts" pseudo-source replays the same dependency probes
//     handleHomeOverview emits, so the feed stays consistent with the KPI
//     strip's alert badge.
package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/activity"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
)

// adminEvent is the typed union the dashboard renders one per row.
//
// Kind is namespaced (`run.failed`, `webhook.delivered`, ...) so the
// dashboard can map to icons without inspecting Source.
type adminEvent struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	ResourceID  string `json:"resource_id,omitempty"`
	OccurredAt  string `json:"occurred_at"`
}

type adminEventListResponse struct {
	Events []adminEvent `json:"events"`
}

const (
	adminEventDefaultLimit = 20
	adminEventMaxLimit     = 100
)

func (s *Server) registerAdminEventsRoutes() {
	// S1b: /api/v1/admin is on publicPrefixes (bypasses the tenant
	// resolver), and this feed aggregates cross-tenant runs, webhook
	// deliveries, and alerts — it must enforce operator auth itself.
	s.mux.HandleFunc("GET /api/v1/admin/events", s.operatorGuard(rbac.ResourceAdminEvents, s.handleAdminEvents))
}

func (s *Server) registerAdminEventsOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/events", openapi.RouteMeta{
		Summary: "Unified events feed (runs + webhooks + alerts + activity)",
		Tags:    []string{"admin"},
	})
}

// handleAdminEvents merges all four sources, sorts by OccurredAt DESC,
// applies the optional kind filter, and trims to `limit`. Each branch is
// independently fault-tolerant so a slow / failing source degrades the
// feed rather than the whole request.
func (s *Server) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.admin.events")
	defer span.End()

	q := r.URL.Query()
	limit := adminEventDefaultLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			if v > adminEventMaxLimit {
				v = adminEventMaxLimit
			}
			limit = v
		}
	}
	kindFilter := parseKindFilter(q.Get("kind"))

	// Each branch contributes its own slice. We deliberately overfetch a
	// little per source (limit * 2) so the post-merge sort doesn't drop
	// recent items from a less-busy source under high run/webhook load.
	perSource := limit * 2
	if perSource < adminEventDefaultLimit {
		perSource = adminEventDefaultLimit
	}

	collected := make([]adminEvent, 0, perSource*4)

	collected = append(collected, s.eventsFromRuns(ctx, perSource)...)
	collected = append(collected, s.eventsFromWebhookDeliveries(ctx, perSource)...)
	collected = append(collected, s.eventsFromActivity(ctx, perSource)...)
	collected = append(collected, s.eventsFromAlerts(ctx)...)

	// Apply kind filter (matches Kind exactly or by source-namespace prefix
	// like "run" or "webhook").
	if len(kindFilter) > 0 {
		filtered := collected[:0]
		for _, ev := range collected {
			ns := ev.Kind
			if idx := strings.Index(ns, "."); idx > 0 {
				ns = ns[:idx]
			}
			if _, ok := kindFilter[ev.Kind]; ok {
				filtered = append(filtered, ev)
				continue
			}
			if _, ok := kindFilter[ns]; ok {
				filtered = append(filtered, ev)
			}
		}
		collected = filtered
	}

	sort.SliceStable(collected, func(i, j int) bool {
		return collected[i].OccurredAt > collected[j].OccurredAt
	})

	if len(collected) > limit {
		collected = collected[:limit]
	}

	writeJSON(w, http.StatusOK, adminEventListResponse{Events: collected})
}

// parseKindFilter accepts a CSV like "run.failed,webhook,alert" and returns
// the lookup set. Empty input means "no filter" (return nil map).
func parseKindFilter(raw string) map[string]struct{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// eventsFromRuns folds recent gateway-request rows into typed events. We
// reuse queryRuns so the filter (endpoint LIKE '/api/v1/execute/%') stays
// consistent with the Runs page.
func (s *Server) eventsFromRuns(ctx context.Context, limit int) []adminEvent {
	if s.db == nil || s.db.Pool == nil {
		return nil
	}
	runs, err := s.queryRuns(ctx, runQuery{Limit: limit, Offset: 0})
	if err != nil {
		s.log.Warn("admin events: runs source failed", "error", err)
		return nil
	}
	out := make([]adminEvent, 0, len(runs.Runs))
	for _, r := range runs.Runs {
		kind := "run.succeeded"
		severity := "info"
		title := "Run succeeded · " + r.Agent
		switch r.Status {
		case "failed":
			kind = "run.failed"
			severity = "warning"
			title = "Run failed · " + r.Agent
		case "running":
			kind = "run.running"
			title = "Run started · " + r.Agent
		case "cancelled":
			kind = "run.cancelled"
			title = "Run cancelled · " + r.Agent
		}
		out = append(out, adminEvent{
			Kind:       kind,
			ID:         "run:" + r.ID,
			Severity:   severity,
			Title:      title,
			Source:     "runs",
			ResourceID: r.ID,
			OccurredAt: r.StartedAt,
		})
	}
	return out
}

// eventsFromWebhookDeliveries folds the most-recent N delivery rows into
// typed events. Uses the same mapping helpers as handleHomeOverview so
// statuses stay consistent across surfaces.
func (s *Server) eventsFromWebhookDeliveries(ctx context.Context, limit int) []adminEvent {
	hooks, err := s.fetchRecentWebhookDeliveries(ctx, limit)
	if err != nil {
		s.log.Warn("admin events: webhooks source failed", "error", err)
		return nil
	}
	out := make([]adminEvent, 0, len(hooks))
	for _, h := range hooks {
		kind := "webhook.pending"
		severity := "info"
		title := "Webhook " + h.Direction + " · " + h.URL
		switch h.Status {
		case "delivered":
			kind = "webhook.delivered"
		case "failed":
			kind = "webhook.failed"
			severity = "warning"
		}
		out = append(out, adminEvent{
			Kind:       kind,
			ID:         "webhook:" + h.ID,
			Severity:   severity,
			Title:      title,
			Source:     "webhooks",
			ResourceID: h.ID,
			OccurredAt: h.OccurredAt,
		})
	}
	return out
}

// eventsFromActivity folds the customer-facing activity log into typed
// events. Skipped silently if no activity store is configured (this is
// common in single-tenant dev installs).
func (s *Server) eventsFromActivity(ctx context.Context, limit int) []adminEvent {
	if s.activity == nil {
		return nil
	}
	// ListAll, not List: this feed is the operator's cross-tenant view and
	// the request context carries no tenant — List rejects that with
	// ErrTenantRequired on every poll. The route is operator-guarded,
	// which is ListAll's contract.
	page, err := s.activity.ListAll(ctx, "", activity.ListFilter{Limit: limit, Offset: 0})
	if err != nil {
		s.log.Warn("admin events: activity source failed", "error", err)
		return nil
	}
	out := make([]adminEvent, 0, len(page.Entries))
	for _, e := range page.Entries {
		title := e.Action
		if e.ResourceType != nil && *e.ResourceType != "" {
			title = e.Action + " · " + *e.ResourceType
		}
		resourceID := ""
		if e.ResourceID != nil {
			resourceID = *e.ResourceID
		}
		out = append(out, adminEvent{
			Kind:       "activity." + e.Action,
			ID:         "activity:" + e.ID,
			Severity:   "info",
			Title:      title,
			Source:     "activity",
			ResourceID: resourceID,
			OccurredAt: e.OccurredAt,
		})
	}
	return out
}

// eventsFromAlerts emits a synthetic event per active dependency alert so
// the feed stays consistent with the KPI strip's red badge. Mirrors the
// probe logic in handleHomeOverview so the two surfaces never disagree.
func (s *Server) eventsFromAlerts(ctx context.Context) []adminEvent {
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	out := []adminEvent{}
	if s.af != nil {
		if _, err := s.af.Health(probeCtx); err != nil {
			out = append(out, adminEvent{
				Kind:        "alert.dependency",
				ID:          "alert:agentfield-unreachable",
				Severity:    "critical",
				Title:       "AgentField unreachable",
				Description: err.Error(),
				Source:      "alerts",
				OccurredAt:  now,
			})
		}
	}
	if s.db != nil {
		if err := s.db.Health(probeCtx); err != nil {
			out = append(out, adminEvent{
				Kind:        "alert.dependency",
				ID:          "alert:database-unhealthy",
				Severity:    "critical",
				Title:       "Database unhealthy",
				Description: err.Error(),
				Source:      "alerts",
				OccurredAt:  now,
			})
		}
	}
	return out
}
