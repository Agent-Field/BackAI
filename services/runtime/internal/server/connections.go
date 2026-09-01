// SPDX-License-Identifier: Apache-2.0

// Connections endpoints (R5) — one secure connection contract for external
// services. App code creates a connection once (API key, or an OAuth consent
// round-trip), then makes provider calls through the HANDLE
// (POST /api/v1/connections/{id}/request): the runtime injects credentials
// server-side and returns the provider's response. Raw credentials are never
// returned to app code and never logged. Tenant identity comes only from the
// resolver; the OAuth callback trusts its signed state.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/connections"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

func (s *Server) registerConnectionsRoutes() {
	s.mux.HandleFunc("POST /api/v1/connections", s.handleConnectionsCreate)
	s.mux.HandleFunc("GET /api/v1/connections", s.handleConnectionsList)
	s.mux.HandleFunc("DELETE /api/v1/connections/{id}", s.handleConnectionsDelete)
	s.mux.HandleFunc("POST /api/v1/connections/{id}/request", s.handleConnectionsRequest)
	s.mux.HandleFunc("POST /api/v1/connections/{id}/verify-webhook", s.handleConnectionsVerifyWebhook)
	// OAuth callback — provider redirects the browser here. Lives under the
	// /connections public prefix (tenant resolution bypassed); the signed
	// state carries the tenant, exactly like /oauth/callback.
	s.mux.HandleFunc("GET /connections/callback/{provider}", s.handleConnectionsCallback)
}

func (s *Server) registerConnectionsOpenAPI() {
	idParam := []openapi.Parameter{{
		Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"},
	}}
	s.openapi.Register("POST", "/api/v1/connections", openapi.RouteMeta{
		Summary: "Create a connection (api_key stores a credential; oauth returns an authorize URL)",
		Tags:    []string{"connections"},
	})
	s.openapi.Register("GET", "/api/v1/connections", openapi.RouteMeta{
		Summary: "List connections (metadata + health, never credentials)", Tags: []string{"connections"},
	})
	s.openapi.Register("DELETE", "/api/v1/connections/{id}", openapi.RouteMeta{
		Summary: "Revoke a connection", Tags: []string{"connections"}, Parameters: idParam,
	})
	s.openapi.Register("POST", "/api/v1/connections/{id}/request", openapi.RouteMeta{
		Summary: "Proxy a provider request with server-injected credentials", Tags: []string{"connections"}, Parameters: idParam,
	})
	s.openapi.Register("POST", "/api/v1/connections/{id}/verify-webhook", openapi.RouteMeta{
		Summary: "Verify an inbound webhook signature for a connection", Tags: []string{"connections"}, Parameters: idParam,
	})
	s.openapi.Register("GET", "/connections/callback/{provider}", openapi.RouteMeta{
		Summary: "OAuth connection callback", Tags: []string{"connections"},
		Parameters: []openapi.Parameter{{Name: "provider", In: "path", Required: true, Schema: map[string]any{"type": "string"}}},
	})
}

// connectionsUnavailable writes a 503 when the subsystem isn't wired.
func (s *Server) connectionsUnavailable(w http.ResponseWriter) bool {
	if s.connections == nil {
		writeError(w, http.StatusServiceUnavailable, "CONNECTIONS_NOT_CONFIGURED",
			"connections subsystem is not configured on this runtime (requires a database + secrets vault)", nil)
		return true
	}
	return false
}

func writeConnectionsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connections.ErrUnknownProvider):
		writeError(w, http.StatusNotFound, "CONNECTION_PROVIDER_UNKNOWN", "unknown provider", nil)
	case errors.Is(err, connections.ErrProviderNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "CONNECTION_PROVIDER_NOT_CONFIGURED",
			"provider OAuth is not configured on this runtime", nil)
	case errors.Is(err, connections.ErrKindUnsupported):
		writeError(w, http.StatusBadRequest, "CONNECTION_KIND_UNSUPPORTED", "connection kind unsupported for this provider", nil)
	case errors.Is(err, connections.ErrNotFound):
		writeError(w, http.StatusNotFound, "CONNECTION_NOT_FOUND", "connection not found", nil)
	case errors.Is(err, connections.ErrRevoked):
		writeError(w, http.StatusConflict, "CONNECTION_REVOKED", "connection has been revoked", nil)
	case errors.Is(err, connections.ErrScopeNotGranted):
		writeError(w, http.StatusForbidden, "CONNECTION_SCOPE_NOT_GRANTED", "connection does not grant the scope required for this call", nil)
	case errors.Is(err, connections.ErrRefreshFailed):
		writeError(w, http.StatusBadGateway, "CONNECTION_REFRESH_FAILED", "OAuth token refresh failed; reconnect the connection", nil)
	case errors.Is(err, connections.ErrWebhookUnsupported):
		writeError(w, http.StatusBadRequest, "CONNECTION_WEBHOOK_UNSUPPORTED", "provider has no verifiable webhook signature scheme", nil)
	case errors.Is(err, connections.ErrInvalidState):
		writeError(w, http.StatusBadRequest, "CONNECTION_INVALID_STATE", "oauth state is invalid or expired", nil)
	case errors.Is(err, connections.ErrCredentialRequired), errors.Is(err, connections.ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "connection request failed", nil)
	}
}

type connectionsCreateInput struct {
	Provider      string   `json:"provider"`
	Kind          string   `json:"kind"`
	Name          string   `json:"name,omitempty"`
	APIKey        string   `json:"api_key,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	WebhookSecret string   `json:"webhook_secret,omitempty"`
	ReturnTo      string   `json:"return_to,omitempty"`
}

type connectionsAuthorizeResponse struct {
	Provider         string `json:"provider"`
	Kind             string `json:"kind"`
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	RedirectURI      string `json:"redirect_uri"`
}

func (s *Server) handleConnectionsCreate(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in connectionsCreateInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	createdBy := connectionsCreatedBy(r)

	switch kind {
	case connections.KindAPIKey:
		conn, err := s.connections.CreateAPIKey(r.Context(), tenantID, connections.CreateAPIKeyParams{
			Provider:      in.Provider,
			Name:          in.Name,
			APIKey:        in.APIKey,
			Scopes:        in.Scopes,
			WebhookSecret: in.WebhookSecret,
			CreatedBy:     createdBy,
		})
		if err != nil {
			writeConnectionsError(w, err)
			return
		}
		s.audit.Write(r.Context(), r, audit.Event{
			Action: "connection.create", ResourceType: "connection", ResourceID: conn.ID,
			Metadata: map[string]any{"provider": conn.Provider, "kind": conn.Kind},
		})
		writeJSON(w, http.StatusCreated, conn)
	case connections.KindOAuth:
		redirectURI := connectionsRedirectURI(r, in.Provider)
		authURL, state, err := s.connections.AuthorizeURL(r.Context(), tenantID, connections.AuthorizeParams{
			Provider:    in.Provider,
			Name:        in.Name,
			Scopes:      in.Scopes,
			ReturnTo:    in.ReturnTo,
			CreatedBy:   createdBy,
			RedirectURI: redirectURI,
		})
		if err != nil {
			writeConnectionsError(w, err)
			return
		}
		s.audit.Write(r.Context(), r, audit.Event{
			Action: "connection.authorize", ResourceType: "connection", ResourceID: strings.ToLower(in.Provider),
			Metadata: map[string]any{"provider": strings.ToLower(in.Provider)},
		})
		writeJSON(w, http.StatusOK, connectionsAuthorizeResponse{
			Provider: strings.ToLower(strings.TrimSpace(in.Provider)), Kind: connections.KindOAuth,
			AuthorizationURL: authURL, State: state, RedirectURI: redirectURI,
		})
	default:
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "kind must be 'api_key' or 'oauth'", nil)
	}
}

func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	conns, err := s.connections.List(r.Context(), tenantID)
	if err != nil {
		writeConnectionsError(w, err)
		return
	}
	if conns == nil {
		conns = []connections.Connection{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

func (s *Server) handleConnectionsDelete(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	if err := s.connections.Revoke(r.Context(), tenantID, id); err != nil {
		writeConnectionsError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action: "connection.revoke", ResourceType: "connection", ResourceID: id,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

type connectionsRequestInput struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

func (s *Server) handleConnectionsRequest(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in connectionsRequestInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	res, err := s.connections.Handle(r.Context(), tenantID, id, connections.HandleRequest{
		Method:  in.Method,
		Path:    in.Path,
		Query:   in.Query,
		Headers: in.Headers,
		Body:    decodeProviderBody(in.Body),
	})
	if err != nil {
		writeConnectionsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  res.Status,
		"headers": res.Headers,
		"body":    providerBodyToJSON(res.Body),
	})
}

type connectionsVerifyWebhookInput struct {
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

func (s *Server) handleConnectionsVerifyWebhook(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
		return
	}
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in connectionsVerifyWebhookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	valid, err := s.connections.VerifyWebhook(r.Context(), tenantID, id, in.Headers, decodeProviderBody(in.Body))
	if err != nil {
		writeConnectionsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"valid": valid})
}

func (s *Server) handleConnectionsCallback(w http.ResponseWriter, r *http.Request) {
	if s.connectionsUnavailable(w) {
		return
	}
	if queryErr := strings.TrimSpace(r.URL.Query().Get("error")); queryErr != "" {
		writeError(w, http.StatusBadRequest, "CONNECTION_PROVIDER_DENIED", queryErr, nil)
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "code and state are required", nil)
		return
	}
	conn, err := s.connections.CompleteOAuth(r.Context(), provider, code, state, connectionsRedirectURI(r, provider))
	if err != nil {
		writeConnectionsError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action: "connection.callback", ResourceType: "connection", ResourceID: conn.ID,
		Metadata: map[string]any{"provider": conn.Provider},
	})
	// Redirect back to the app when return_to was supplied at authorize time
	// (encoded into the signed state) is not surfaced here; the connection is
	// created and the app can poll GET /connections. Return JSON otherwise.
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "connection": conn})
}

// connectionsCreatedBy attributes a connection to the acting principal.
func connectionsCreatedBy(r *http.Request) string {
	if u := tenantctx.UserID(r.Context()); u != "" {
		return u
	}
	return tenantctx.APIKeyID(r.Context())
}

// connectionsRedirectURI builds the OAuth callback URL for a provider,
// mirroring the oauth-OBO redirect derivation (AF_STACK_PUBLIC_URL or the
// forwarded host).
func connectionsRedirectURI(r *http.Request, provider string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("AF_STACK_PUBLIC_URL")), "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
			scheme = proto
		}
		host := r.Host
		if fh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fh != "" {
			host = fh
		}
		base = scheme + "://" + host
	}
	return base + "/connections/callback/" + url.PathEscape(strings.ToLower(strings.TrimSpace(provider)))
}

// decodeProviderBody turns the JSON `body` field into raw bytes for the
// provider request. A JSON string is forwarded as its unquoted content (so
// apps can send form-encoded or plain-text bodies); any other JSON value is
// forwarded verbatim.
func decodeProviderBody(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []byte(asString)
	}
	return []byte(raw)
}

// providerBodyToJSON embeds a provider response body into the JSON envelope:
// valid JSON is inlined; anything else becomes a JSON string.
func providerBodyToJSON(body []byte) json.RawMessage {
	if len(body) == 0 {
		return json.RawMessage("null")
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	quoted, err := json.Marshal(string(body))
	if err != nil {
		return json.RawMessage("null")
	}
	return quoted
}
