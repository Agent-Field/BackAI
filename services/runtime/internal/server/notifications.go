// SPDX-License-Identifier: Apache-2.0

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

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
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
	ByStatus    map[string]int               `json:"by_status"`
	ByAdapter   []notifications.AdapterCount `json:"by_adapter"`
	SentToday   int                          `json:"sent_today"`
	FailedToday int                          `json:"failed_today"`
}

type createNotificationMuteInput struct {
	TenantID  string                    `json:"tenant_id,omitempty"`
	Pattern   notifications.MutePattern `json:"pattern"`
	Reason    string                    `json:"reason,omitempty"`
	ExpiresAt *string                   `json:"expires_at,omitempty"`
}

type notificationChannelInput struct {
	ID         string         `json:"id,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	ConfigJSON map[string]any `json:"config_json,omitempty"`
	Enabled    *bool          `json:"enabled,omitempty"`
}

type notificationChannelListResponse struct {
	Channels []notifications.Channel `json:"channels"`
}

// ─── Registration ─────────────────────────────────────────────────────────

func (s *Server) registerNotificationsRoutes() {
	s.mux.HandleFunc("POST /api/v1/notifications", s.handleSendNotification)
	// List/stats/get read across the outbox and (before this guard) scoped
	// only by a client-supplied ?tenant param — an unauthenticated IDOR that
	// leaked other tenants' notifications. This surface is on the
	// /api/v1/notifications public prefix; gate the cross-tenant reads to
	// operators. Send stays open for the internal (header-only) agent path.
	s.mux.HandleFunc("GET /api/v1/notifications", s.operatorGuard(rbac.ResourceAdminActivity, s.handleListNotifications))
	s.mux.HandleFunc("GET /api/v1/notifications/stats", s.operatorGuard(rbac.ResourceAdminActivity, s.handleNotificationsStats))
	s.mux.HandleFunc("GET /api/v1/notifications/channels", s.handleListNotificationChannels)
	s.mux.HandleFunc("POST /api/v1/notifications/channels", s.handleUpsertNotificationChannel)
	s.mux.HandleFunc("PATCH /api/v1/notifications/channels", s.handleUpsertNotificationChannel)
	s.mux.HandleFunc("DELETE /api/v1/notifications/channels", s.handleDeleteNotificationChannel)
	s.mux.HandleFunc("GET /api/v1/notifications/mutes", s.handleListNotificationMutes)
	s.mux.HandleFunc("POST /api/v1/notifications/mutes", s.handleCreateNotificationMute)
	s.mux.HandleFunc("DELETE /api/v1/notifications/mutes/{id}", s.handleDeleteNotificationMute)
	s.mux.HandleFunc("GET /api/v1/notifications/{id}", s.operatorGuard(rbac.ResourceAdminActivity, s.handleGetNotification))
}

func (s *Server) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		writeJSON(w, http.StatusOK, notificationChannelListResponse{Channels: []notifications.Channel{}})
		return
	}
	channels, err := s.notifications.Channels(r.Context())
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	if channels == nil {
		channels = []notifications.Channel{}
	}
	writeJSON(w, http.StatusOK, notificationChannelListResponse{Channels: channels})
}

func (s *Server) handleUpsertNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		writeNotificationError(w, notifications.ErrNotConfigured)
		return
	}
	var in notificationChannelInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	ch, err := s.notifications.UpsertChannel(r.Context(), notifications.ChannelInput{
		Kind:    notifications.Kind(strings.ToLower(strings.TrimSpace(in.Kind))),
		Config:  in.ConfigJSON,
		Enabled: enabled,
	})
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	action := "notification_channel.upsert"
	if r.Method == http.MethodPatch {
		action = "notification_channel.patch"
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       action,
		ResourceType: "notification_channel",
		ResourceID:   ch.ID,
		Metadata: map[string]any{
			"kind":    ch.Kind,
			"enabled": ch.Enabled,
		},
	})
	writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		writeNotificationError(w, notifications.ErrNotConfigured)
		return
	}
	var in notificationChannelInput
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in)
	}
	if in.ID == "" {
		in.ID = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if in.Kind == "" {
		in.Kind = strings.TrimSpace(r.URL.Query().Get("kind"))
	}
	if err := s.notifications.DeleteChannel(r.Context(), in.ID, in.Kind); err != nil {
		writeNotificationError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "notification_channel.delete",
		ResourceType: "notification_channel",
		ResourceID:   strings.TrimSpace(in.ID),
		Metadata: map[string]any{
			"kind": strings.TrimSpace(in.Kind),
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	out, err := s.notifications.Get(ctx, id)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListNotificationMutes(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.mutes.list")
	defer span.End()
	if s.notifications == nil {
		writeJSON(w, http.StatusOK, notifications.MuteListResult{Mutes: []notifications.Mute{}})
		return
	}
	tenant := strings.TrimSpace(r.URL.Query().Get("tenant"))
	out, err := s.notifications.ListMutes(ctx, tenant)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	if out.Mutes == nil {
		out.Mutes = []notifications.Mute{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateNotificationMute(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.mutes.create")
	defer span.End()
	if s.notifications == nil {
		writeNotificationError(w, notifications.ErrNotConfigured)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in createNotificationMuteInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	create := notifications.CreateMuteInput{
		TenantID: s.defaultTenant(r),
		Pattern:  in.Pattern,
		Reason:   in.Reason,
	}
	if strings.TrimSpace(in.TenantID) != "" {
		create.TenantID = strings.TrimSpace(in.TenantID)
	}
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := parseRFC3339(*in.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "expires_at must be RFC3339", nil)
			return
		}
		create.ExpiresAt = &t
	}
	out, err := s.notifications.CreateMute(ctx, create)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "notification_mute.create",
		ResourceType: "notification_mute",
		ResourceID:   out.ID,
		Metadata: map[string]any{
			"pattern": out.Pattern,
		},
	})
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleDeleteNotificationMute(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.notifications.mutes.delete")
	defer span.End()
	if s.notifications == nil {
		writeNotificationError(w, notifications.ErrNotConfigured)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	if err := s.notifications.DeleteMute(ctx, id); err != nil {
		writeNotificationError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "notification_mute.delete",
		ResourceType: "notification_mute",
		ResourceID:   id,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
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
	b.Register("GET", "/api/v1/notifications/channels", openapi.RouteMeta{
		Summary: "List notification channel configuration", Tags: []string{"notifications"},
	})
	b.Register("POST", "/api/v1/notifications/channels", openapi.RouteMeta{
		Summary: "Create or replace notification channel configuration", Tags: []string{"notifications"},
	})
	b.Register("PATCH", "/api/v1/notifications/channels", openapi.RouteMeta{
		Summary: "Update notification channel configuration", Tags: []string{"notifications"},
	})
	b.Register("DELETE", "/api/v1/notifications/channels", openapi.RouteMeta{
		Summary: "Delete notification channel configuration", Tags: []string{"notifications"},
	})
	b.Register("GET", "/api/v1/notifications/mutes", openapi.RouteMeta{
		Summary: "List notification mute rules", Tags: []string{"notifications"},
	})
	b.Register("POST", "/api/v1/notifications/mutes", openapi.RouteMeta{
		Summary: "Create a notification mute rule", Tags: []string{"notifications"},
	})
	b.Register("DELETE", "/api/v1/notifications/mutes/{id}", openapi.RouteMeta{
		Summary: "Delete a notification mute rule", Tags: []string{"notifications"},
	})
	b.Register("GET", "/api/v1/notifications/{id}", openapi.RouteMeta{
		Summary: "Get a single notification", Tags: []string{"notifications"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}
