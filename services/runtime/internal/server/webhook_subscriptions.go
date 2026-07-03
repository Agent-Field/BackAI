// SPDX-License-Identifier: Apache-2.0

// webhook_subscriptions.go — TENANT-SCOPED outbound webhook surface.
//
// Unlike the operator-gated /api/v1/webhooks/{endpoints,send,deliveries}
// routes (which bypass the tenant resolver and self-enforce operator auth),
// these routes go THROUGH the tenant resolver (see the isPublicPath
// exception in tenant_resolver.go) so app.tenant_id is bound per request.
// A tenant registers its own subscriber URLs and emits its own events; RLS
// + the emit fan-out scope everything to the caller's tenant. This is the
// "the app is a server that sends + receives webhooks" surface — scoped
// delivery, NOT the open outbound relay /send guards against.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/webhooks"
)

// registerWebhookSubscriptionRoutes wires the tenant-scoped subscribe +
// emit surface. No operatorGuard: auth is the caller's tenant binding.
func (s *Server) registerWebhookSubscriptionRoutes() {
	s.mux.HandleFunc("POST /api/v1/webhooks/subscriptions", s.handleCreateSubscription)
	s.mux.HandleFunc("GET /api/v1/webhooks/subscriptions", s.handleListSubscriptions)
	s.mux.HandleFunc("DELETE /api/v1/webhooks/subscriptions/{id}", s.handleDeleteSubscription)
	s.mux.HandleFunc("POST /api/v1/webhooks/emit", s.handleEmitWebhook)

	b := s.openapi
	b.Register("POST", "/api/v1/webhooks/subscriptions", openapi.RouteMeta{
		Summary: "Register a tenant outbound webhook subscriber", Tags: []string{"webhooks"},
	})
	b.Register("GET", "/api/v1/webhooks/subscriptions", openapi.RouteMeta{
		Summary: "List the caller tenant's webhook subscriptions", Tags: []string{"webhooks"},
	})
	b.Register("DELETE", "/api/v1/webhooks/subscriptions/{id}", openapi.RouteMeta{
		Summary: "Delete a webhook subscription", Tags: []string{"webhooks"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/webhooks/emit", openapi.RouteMeta{
		Summary: "Emit an event to the caller tenant's webhook subscribers", Tags: []string{"webhooks"},
	})
}

// subscriptionsStore returns the wired store or nil.
func (s *Server) subscriptionsStore() *webhooks.SubscriptionStore {
	if s.webhooks == nil {
		return nil
	}
	st := s.webhooks.Subscriptions()
	if st == nil || !st.HasPool() {
		return nil
	}
	return st
}

// requireTenant reads the resolved tenant or writes a 401 + returns "".
func (s *Server) requireTenant(w http.ResponseWriter, r *http.Request) string {
	tenant := strings.TrimSpace(tenantctx.TenantID(r.Context()))
	if tenant == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED",
			"a tenant session or API key is required", nil)
	}
	return tenant
}

type createSubscriptionInput struct {
	URL    string   `json:"url"`
	Events []string `json:"events,omitempty"`
}

func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	store := s.subscriptionsStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_CONFIGURED",
			"webhook subscriptions are not configured on this runtime", nil)
		return
	}
	if s.requireTenant(w, r) == "" {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in createSubscriptionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	sub, err := store.Create(r.Context(), in.URL, in.Events)
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	// Secret is returned exactly once, here.
	writeJSON(w, http.StatusCreated, sub)
}

type subscriptionListResponse struct {
	Subscriptions []webhooks.Subscription `json:"subscriptions"`
}

func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	store := s.subscriptionsStore()
	if store == nil {
		writeJSON(w, http.StatusOK, subscriptionListResponse{Subscriptions: []webhooks.Subscription{}})
		return
	}
	if s.requireTenant(w, r) == "" {
		return
	}
	subs, err := store.List(r.Context())
	if err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, subscriptionListResponse{Subscriptions: subs})
}

func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	store := s.subscriptionsStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_CONFIGURED",
			"webhook subscriptions are not configured on this runtime", nil)
		return
	}
	if s.requireTenant(w, r) == "" {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	if err := store.Delete(r.Context(), id); err != nil {
		writeWebhookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

type emitWebhookInput struct {
	EventType string          `json:"event_type"`
	Body      json.RawMessage `json:"body"`
}

type emitWebhookResponse struct {
	Emitted     int      `json:"emitted"`
	DeliveryIDs []string `json:"delivery_ids"`
}

func (s *Server) handleEmitWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil || s.subscriptionsStore() == nil {
		writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_CONFIGURED",
			"webhook subscriptions are not configured on this runtime", nil)
		return
	}
	tenant := s.requireTenant(w, r)
	if tenant == "" {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in emitWebhookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	if strings.TrimSpace(in.EventType) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "event_type is required", nil)
		return
	}
	count, ids, err := s.webhooks.Emit(r.Context(), tenant, in.EventType, []byte(in.Body))
	if err != nil {
		if errors.Is(err, webhooks.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "WEBHOOKS_NOT_CONFIGURED",
				"outbound webhook delivery is not configured on this runtime", nil)
			return
		}
		writeWebhookError(w, err)
		return
	}
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, http.StatusOK, emitWebhookResponse{Emitted: count, DeliveryIDs: ids})
}
