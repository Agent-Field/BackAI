// webhooks.go — REST handlers for the webhook outbox + inbox.
//
// This file is CO-OWNED between Phase 10.2 and Phase 10.3. The two
// halves are clearly demarcated by section markers so they merge cleanly
// when Phase 10.2 lands.
//
// Phase 10.2 (inbound, separate PR) will add:
//   - POST /webhooks/in/{slug}      (raw payload landing — verifies HMAC)
//   - GET    /api/v1/webhooks/endpoints, POST/DELETE for create/delete
//
// Phase 10.3 (this file) adds the OUTBOUND surface + the unified
// /api/v1/webhooks/deliveries list that the dashboard uses:
//   - POST /api/v1/webhooks/send
//   - GET    /api/v1/webhooks/deliveries          (filters: direction, ...)
//   - GET    /api/v1/webhooks/deliveries/{id}
//   - POST /api/v1/webhooks/deliveries/{id}/retry
//
// Endpoint paths and JSON shapes map 1:1 to WebhookDeliverySchema /
// WebhookDeliveryListSchema / SendWebhookInputSchema in
// apps/dashboard/src/lib/api.ts. Any drift here breaks the dashboard's
// safeParse — keep field names snake_case (the webhooks.Delivery struct
// already carries them), keep nullable fields emitted as JSON null
// (pointer fields), and keep arrays non-nil even when empty.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/webhooks"
)

// ─── Phase 10.3: OUTBOUND HANDLERS ───────────────────────────────────────

// sendWebhookInput mirrors SendWebhookInputSchema. Body is a json.RawMessage
// so we forward the caller's payload bytes verbatim (the SDK passes any
// JSON-serialisable value).
type sendWebhookInput struct {
	URL       string            `json:"url"`
	EventType string            `json:"event_type"`
	Body      json.RawMessage   `json:"body"`
	Headers   map[string]string `json:"headers,omitempty"`
	Immediate bool              `json:"immediate,omitempty"`
}

// webhookDeliveryListResponse mirrors WebhookDeliveryListSchema.
type webhookDeliveryListResponse struct {
	Deliveries []webhooks.Delivery `json:"deliveries"`
	Total      int                 `json:"total"`
	HasMore    bool                `json:"has_more"`
}

// registerWebhooksRoutes wires the /api/v1/webhooks/* endpoints
// contributed by Phase 10.2 (inbound + endpoint CRUD) and Phase 10.3
// (outbound send/retry). The list + get endpoints are shared.
func (s *Server) registerWebhooksRoutes() {
	// Phase 10.2: public inbound surface. NOT under /api/v1/; the
	// tenant resolver bypasses it via the /webhooks/ prefix in
	// publicPrefixes.
	s.mux.HandleFunc("POST /webhooks/in/{slug}", s.handleInboundWebhook)

	// Phase 10.2: dashboard endpoint CRUD.
	s.mux.HandleFunc("GET /api/v1/webhooks/endpoints", s.handleListWebhookEndpoints)
	s.mux.HandleFunc("POST /api/v1/webhooks/endpoints", s.handleCreateWebhookEndpoint)
	s.mux.HandleFunc("DELETE /api/v1/webhooks/endpoints/{id}", s.handleDeleteWebhookEndpoint)

	// Outbound (Phase 10.3).
	s.mux.HandleFunc("POST /api/v1/webhooks/send", s.handleWebhookSend)
	s.mux.HandleFunc("GET /api/v1/webhooks/deliveries", s.handleWebhookListDeliveries)
	s.mux.HandleFunc("GET /api/v1/webhooks/deliveries/{id}", s.handleWebhookGetDelivery)
	s.mux.HandleFunc("POST /api/v1/webhooks/deliveries/{id}/retry", s.handleWebhookRetryDelivery)
}

// registerWebhooksOpenAPI describes the routes Phases 10.2 + 10.3 add.
func (s *Server) registerWebhooksOpenAPI() {
	b := s.openapi
	b.AddTag("webhooks", "Webhook outbox + inbox")

	// Inbound (Phase 10.2).
	b.Register("POST", "/webhooks/in/{slug}", openapi.RouteMeta{
		Summary: "Public inbound webhook receiver", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "slug", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("GET", "/api/v1/webhooks/endpoints", openapi.RouteMeta{
		Summary: "List webhook endpoints", Tags: []string{"webhooks"},
	})
	b.Register("POST", "/api/v1/webhooks/endpoints", openapi.RouteMeta{
		Summary: "Create an inbound webhook endpoint", Tags: []string{"webhooks"},
	})
	b.Register("DELETE", "/api/v1/webhooks/endpoints/{id}", openapi.RouteMeta{
		Summary: "Delete a webhook endpoint", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})

	// Outbound (Phase 10.3).
	b.Register("POST", "/api/v1/webhooks/send", openapi.RouteMeta{
		Summary: "Enqueue an outbound webhook delivery", Tags: []string{"webhooks"},
	})
	b.Register("GET", "/api/v1/webhooks/deliveries", openapi.RouteMeta{
		Summary: "List webhook deliveries (inbound + outbound)", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "direction", In: "query",
				Description: "inbound | outbound",
				Schema:      map[string]any{"type": "string"}},
			{Name: "endpoint_id", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "status", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/webhooks/deliveries/{id}", openapi.RouteMeta{
		Summary: "Fetch a single webhook delivery", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/webhooks/deliveries/{id}/retry", openapi.RouteMeta{
		Summary: "Re-queue a delivery for the retry worker", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
}

// outboundUnavailable returns true (and writes a 503) when the
// outbound webhook service is not wired. Used by the Phase 10.3
// mutating endpoints (/send, /retry).
func (s *Server) outboundUnavailable(w http.ResponseWriter) bool {
	if s.webhooks == nil || !s.webhooks.Configured() {
		writeError(w, http.StatusServiceUnavailable,
			"WEBHOOKS_NOT_CONFIGURED",
			"webhooks module is not configured on this runtime", nil)
		return true
	}
	return false
}

// endpointsUnavailable returns true (and writes a 503) when the
// endpoint store is not wired. Used by the Phase 10.2 endpoint CRUD.
func (s *Server) endpointsUnavailable(w http.ResponseWriter) bool {
	if s.webhooks == nil || s.webhooks.Endpoints() == nil || !s.webhooks.Endpoints().HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"WEBHOOKS_NOT_CONFIGURED",
			"webhooks module is not configured on this runtime", nil)
		return true
	}
	return false
}

// deliveriesStore returns the underlying DeliveryStore or nil. Used by
// the list/get delivery handlers — both degrade to tolerant empty
// responses when nil rather than 503.
func (s *Server) deliveriesStore() *webhooks.DeliveryStore {
	if s.webhooks == nil {
		return nil
	}
	st := s.webhooks.Store()
	if st == nil || !st.HasPool() {
		return nil
	}
	return st
}

// writeWebhookError maps webhook errors to HTTP status codes.
func writeWebhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhooks.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, webhooks.ErrUnsupportedForward):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, webhooks.ErrSlugConflict):
		writeError(w, http.StatusConflict, "SLUG_CONFLICT", err.Error(), nil)
	case errors.Is(err, webhooks.ErrEndpointNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "webhook endpoint not found", nil)
	case errors.Is(err, webhooks.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "webhook delivery not found", nil)
	case errors.Is(err, webhooks.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"WEBHOOKS_NOT_CONFIGURED",
			"webhooks module is not configured on this runtime", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── POST /api/v1/webhooks/send ──────────────────────────────────────────

func (s *Server) handleWebhookSend(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.webhooks.send")
	defer span.End()
	if s.outboundUnavailable(w) {
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in sendWebhookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	// Body is forwarded as raw bytes so the upstream sees exactly what
	// the caller wrote. Missing body -> JSON null.
	bodyBytes := []byte(in.Body)
	if len(bodyBytes) == 0 {
		bodyBytes = []byte("null")
	}

	delivery, err := s.webhooks.Enqueue(ctx, webhooks.SendInput{
		URL:       in.URL,
		EventType: in.EventType,
		Body:      bodyBytes,
		Headers:   in.Headers,
		Immediate: in.Immediate,
		TenantID:  s.defaultTenant(r),
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, delivery)
}

// ─── GET /api/v1/webhooks/deliveries ─────────────────────────────────────

func (s *Server) handleWebhookListDeliveries(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.webhooks.list_deliveries")
	defer span.End()

	resp := webhookDeliveryListResponse{Deliveries: []webhooks.Delivery{}}
	// Tolerant: when no store is wired we still serve an empty page so
	// the dashboard renders the empty state instead of 503. Mutating
	// endpoints (send, retry) keep the strict 503 contract.
	store := s.deliveriesStore()
	if store == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filters := webhooks.ListFilters{
		Direction:  webhooks.Direction(strings.TrimSpace(q.Get("direction"))),
		EndpointID: strings.TrimSpace(q.Get("endpoint_id")),
		TenantID:   strings.TrimSpace(q.Get("tenant")),
		Status:     strings.TrimSpace(q.Get("status")),
		Limit:      limit,
		Offset:     offset,
	}
	res, err := store.List(ctx, filters)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	if res.Deliveries != nil {
		resp.Deliveries = res.Deliveries
	}
	resp.Total = res.Total
	resp.HasMore = res.HasMore
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/webhooks/deliveries/{id} ────────────────────────────────

func (s *Server) handleWebhookGetDelivery(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.webhooks.get_delivery")
	defer span.End()
	store := s.deliveriesStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"webhook delivery not found", nil)
		return
	}
	id := r.PathValue("id")
	d, err := store.Get(ctx, id)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ─── POST /api/v1/webhooks/deliveries/{id}/retry ─────────────────────────

func (s *Server) handleWebhookRetryDelivery(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.webhooks.retry_delivery")
	defer span.End()
	if s.outboundUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	d, err := s.webhooks.Retry(ctx, id)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// ─── Phase 10.2: INBOUND HANDLERS ────────────────────────────────────────

// createEndpointInput mirrors CreateEndpointInputSchema in api.ts.
// Pointer fields stand in for "omitted" so the handler can apply
// schema defaults rather than treat the empty string as the caller
// explicitly choosing it.
type createEndpointInput struct {
	Slug               string  `json:"slug"`
	Provider           string  `json:"provider"`
	ForwardTo          string  `json:"forward_to"`
	SecretKeyRef       *string `json:"secret_key_ref,omitempty"`
	SignatureAlgorithm string  `json:"signature_algorithm,omitempty"`
	SignatureHeader    string  `json:"signature_header,omitempty"`
	DedupWindowS       int     `json:"dedup_window_s,omitempty"`
}

// webhookEndpointListResponse mirrors WebhookEndpointListSchema.
type webhookEndpointListResponse struct {
	Endpoints []webhooks.Endpoint `json:"endpoints"`
}

// ─── POST /webhooks/in/{slug} ─────────────────────────────────────────────

func (s *Server) handleInboundWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "webhooks.inbound")
	defer span.End()

	inbound := s.webhooks.Inbound()
	if inbound == nil || !inbound.Configured() {
		writeError(w, http.StatusServiceUnavailable,
			"WEBHOOKS_NOT_CONFIGURED",
			"webhooks module is not configured on this runtime", nil)
		return
	}

	slug := r.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "slug required", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}

	// Flatten request headers into a string map. The inbound dedup +
	// verify logic is case-insensitive, so first-value-per-key is fine.
	hdrs := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) == 0 {
			continue
		}
		hdrs[k] = v[0]
	}

	delivery, found, handleErr := inbound.Handle(ctx, slug, hdrs, body)
	if !found {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"webhook endpoint not found", nil)
		return
	}
	if handleErr != nil {
		s.log.Warn("webhooks: inbound handle failed",
			"slug", slug, "error", handleErr)
		// Still 200 — providers retry on 5xx and we don't want our
		// internal hiccup to amplify their traffic. The failure is
		// recorded on the delivery row for the dashboard.
		writeJSON(w, http.StatusOK, map[string]any{
			"received": true,
			"status":   "failed",
		})
		return
	}
	resp := map[string]any{"received": true}
	if delivery != nil {
		resp["status"] = string(delivery.Status)
		resp["delivery_id"] = delivery.ID
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/webhooks/endpoints ───────────────────────────────────────

func (s *Server) handleListWebhookEndpoints(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "webhooks.endpoints.list")
	defer span.End()

	// Tolerant empty response when no store is wired.
	store := s.webhooks
	if store == nil || store.Endpoints() == nil || !store.Endpoints().HasPool() {
		writeJSON(w, http.StatusOK,
			webhookEndpointListResponse{Endpoints: []webhooks.Endpoint{}})
		return
	}
	eps, err := store.Endpoints().List(ctx, webhooks.ListEndpointsFilter{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant")),
	})
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	if eps == nil {
		eps = []webhooks.Endpoint{}
	}
	writeJSON(w, http.StatusOK,
		webhookEndpointListResponse{Endpoints: eps})
}

// ─── POST /api/v1/webhooks/endpoints ──────────────────────────────────────

func (s *Server) handleCreateWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "webhooks.endpoints.create")
	defer span.End()
	if s.endpointsUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in createEndpointInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}

	create := webhooks.CreateEndpointInput{
		Slug:               in.Slug,
		Provider:           in.Provider,
		ForwardTo:          in.ForwardTo,
		SecretKeyRef:       in.SecretKeyRef,
		SignatureAlgorithm: in.SignatureAlgorithm,
		SignatureHeader:    in.SignatureHeader,
		DedupWindowS:       in.DedupWindowS,
		// Endpoints are tenant-scoped to the resolved request tenant.
		// When MT is off this is the default tenant.
		TenantID: s.defaultTenant(r),
	}
	ep, err := s.webhooks.Endpoints().Create(ctx, create)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

// ─── DELETE /api/v1/webhooks/endpoints/{id} ───────────────────────────────

func (s *Server) handleDeleteWebhookEndpoint(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "webhooks.endpoints.delete")
	defer span.End()
	if s.endpointsUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if err := s.webhooks.Endpoints().Delete(ctx, id); err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// _ keeps the context import live (used by handler contexts above).
var _ = context.Background
