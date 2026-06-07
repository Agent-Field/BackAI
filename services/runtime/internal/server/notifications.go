// notifications.go — REST handlers for the suite notifications outbox.
//
// Endpoints map 1:1 to NotificationSchema / NotificationListSchema /
// NotificationStatsSchema / SendNotificationInputSchema in
// apps/dashboard/src/lib/api.ts. Any drift breaks the dashboard's
// safeParse — keep field names snake_case (the notifications package
// already carries them), keep nullable fields emitted as JSON null (Go
// nil pointer), and keep arrays + maps non-nil even when empty.
//
// The handlers delegate to notifications.Service which wraps an adapter
// (log / Resend / …) + a Recorder (suite_notifications). When the
// Service isn't wired (no DB / no adapter at boot), reads return
// tolerant empty responses and POST returns 503 NOTIFICATIONS_NOT_CONFIGURED.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// sendNotificationInput mirrors SendNotificationInputSchema in api.ts.
type sendNotificationInput struct {
	Kind        string         `json:"kind"`
	Template    string         `json:"template"`
	To          string         `json:"to"`
	From        string         `json:"from,omitempty"`
	Subject     string         `json:"subject,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
	ScheduledAt string         `json:"scheduled_at,omitempty"`
}

// notificationListResponse mirrors NotificationListSchema.
type notificationListResponse struct {
	Notifications []notifications.Notification `json:"notifications"`
	Total         int                          `json:"total"`
	HasMore       bool                         `json:"has_more"`
}

// notificationStatsResponse mirrors NotificationStatsSchema.
//
// The wire schema declares by_status as a Record<NotificationStatus,
// number>, which means missing entries should be absent (not present
// with the value 0). We emit only the statuses with non-zero counts;
// the dashboard treats absent entries as 0.
type notificationStatsResponse struct {
	ByStatus    map[string]int                 `json:"by_status"`
	ByAdapter   []notifications.AdapterCount   `json:"by_adapter"`
	SentToday   int                            `json:"sent_today"`
	FailedToday int                            `json:"failed_today"`
}

// ─── Registration ─────────────────────────────────────────────────────────

func (s *Server) registerNotificationsRoutes() {
	s.mux.HandleFunc("POST /api/v1/notifications", s.handleSendNotification)
	s.mux.HandleFunc("GET /api/v1/notifications", s.handleListNotifications)
	s.mux.HandleFunc("GET /api/v1/notifications/stats", s.handleNotificationsStats)
	s.mux.HandleFunc("GET /api/v1/notifications/{id}", s.handleGetNotification)
}

func writeNotificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, notifications.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, notifications.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "notification not found", nil)
	case errors.Is(err, notifications.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"NOTIFICATIONS_NOT_CONFIGURED",
			"notifications module is not configured on this runtime", nil)
	case errors.Is(err, notifications.ErrAdapter):
		writeError(w, http.StatusBadGateway, "NOTIFICATIONS_ADAPTER_ERROR", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── POST /api/v1/notifications ───────────────────────────────────────────

func (s *Server) handleSendNotification(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.send")
	defer span.End()
	if s.notifications == nil || !s.notifications.AdapterAvailable() {
		writeError(w, http.StatusServiceUnavailable,
			"NOTIFICATIONS_NOT_CONFIGURED",
			"notifications module is not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in sendNotificationInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	send := notifications.SendInput{
		TenantID: s.defaultTenant(r),
		Kind:     notifications.Kind(strings.ToLower(strings.TrimSpace(in.Kind))),
		Template: in.Template,
		To:       in.To,
		From:     in.From,
		Subject:  in.Subject,
		Data:     in.Data,
	}
	if in.ScheduledAt != "" {
		t, err := parseRFC3339(in.ScheduledAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"scheduled_at must be RFC3339: "+err.Error(), nil)
			return
		}
		send.ScheduledAt = &t
	}

	out, err := s.notifications.Send(ctx, send)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── GET /api/v1/notifications ────────────────────────────────────────────

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.list")
	defer span.End()

	resp := notificationListResponse{Notifications: []notifications.Notification{}}
	if s.notifications == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filters := notifications.ListFilters{
		TenantID: strings.TrimSpace(q.Get("tenant")),
		Status:   strings.TrimSpace(q.Get("status")),
		Kind:     strings.TrimSpace(q.Get("kind")),
		Limit:    limit,
		Offset:   offset,
	}
	res, err := s.notifications.List(ctx, filters)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	resp.Total = res.Total
	resp.HasMore = res.HasMore
	if res.Notifications != nil {
		resp.Notifications = res.Notifications
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/notifications/stats ──────────────────────────────────────

func (s *Server) handleNotificationsStats(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.stats")
	defer span.End()

	empty := notificationStatsResponse{
		ByStatus:  map[string]int{},
		ByAdapter: []notifications.AdapterCount{},
	}
	if s.notifications == nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	tenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	stats, err := s.notifications.Stats(ctx, tenant)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	out := notificationStatsResponse{
		ByStatus:    map[string]int{},
		ByAdapter:   stats.ByAdapter,
		SentToday:   stats.SentToday,
		FailedToday: stats.FailedToday,
	}
	for k, v := range stats.ByStatus {
		out.ByStatus[string(k)] = v
	}
	if out.ByAdapter == nil {
		out.ByAdapter = []notifications.AdapterCount{}
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── GET /api/v1/notifications/{id} ───────────────────────────────────────

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.get")
	defer span.End()
	if s.notifications == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "notification not found", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	out, err := s.notifications.Get(ctx, id)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── OpenAPI ──────────────────────────────────────────────────────────────

func (s *Server) registerNotificationsOpenAPI() {
	b := s.openapi
	b.AddTag("notifications", "Outbox-style notifications (email/sms/push)")
	b.Register("POST", "/api/v1/notifications", openapi.RouteMeta{
		Summary: "Queue a notification for delivery", Tags: []string{"notifications"},
	})
	b.Register("GET", "/api/v1/notifications", openapi.RouteMeta{
		Summary: "List notifications", Tags: []string{"notifications"},
		Parameters: []openapi.Parameter{
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "status", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "kind", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/notifications/stats", openapi.RouteMeta{
		Summary: "Notification KPI aggregates", Tags: []string{"notifications"},
	})
	b.Register("GET", "/api/v1/notifications/{id}", openapi.RouteMeta{
		Summary: "Get a single notification", Tags: []string{"notifications"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}
