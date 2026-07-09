// SPDX-License-Identifier: Apache-2.0

// OAuth-on-behalf-of-user endpoints. These are AF Stack app-auth
// primitives: user grants, token references, and vault-backed token
// retrieval for backend agents. AgentField still owns AI runs, memory,
// spans, traces, and tool-call state.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

type oauthProviderListResponse struct {
	Providers []oauth.ProviderInfo `json:"providers"`
}

type oauthAuthorizeInput struct {
	Scopes   []string `json:"scopes,omitempty"`
	ReturnTo string   `json:"return_to,omitempty"`
}

type oauthAuthorizeResponse struct {
	Provider         string   `json:"provider"`
	AuthorizationURL string   `json:"authorization_url"`
	RedirectURI      string   `json:"redirect_uri"`
	Scopes           []string `json:"scopes"`
}

type oauthConnectionListResponse struct {
	Connections []oauth.Connection `json:"connections"`
}

type oauthTokenInput struct {
	Provider string `json:"provider"`
	UserID   string `json:"user_id,omitempty"`
}

type oauthTokenResponse struct {
	Provider    string     `json:"provider"`
	UserID      string     `json:"user_id"`
	AccessToken string     `json:"access_token"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

func (s *Server) registerOAuthRoutes() {
	s.mux.HandleFunc("GET /api/v1/oauth/providers", s.handleOAuthProviders)
	s.mux.HandleFunc("GET /api/v1/oauth/connections", s.handleOAuthConnections)
	s.mux.HandleFunc("POST /api/v1/oauth/{provider}/authorize", s.handleOAuthAuthorize)
	s.mux.HandleFunc("DELETE /api/v1/oauth/{provider}", s.handleOAuthDisconnect)
	s.mux.HandleFunc("POST /api/v1/oauth/token", s.handleOAuthToken)
	s.mux.HandleFunc("GET /oauth/callback/{provider}", s.handleOAuthCallback)
}

func (s *Server) registerOAuthOpenAPI() {
	s.openapi.Register("GET", "/api/v1/oauth/providers", openapi.RouteMeta{
		Summary: "List OAuth OBO providers", Tags: []string{"oauth"},
	})
	s.openapi.Register("GET", "/api/v1/oauth/connections", openapi.RouteMeta{
		Summary: "List user OAuth OBO connections", Tags: []string{"oauth"},
	})
	s.openapi.Register("POST", "/api/v1/oauth/{provider}/authorize", openapi.RouteMeta{
		Summary: "Create an OAuth OBO authorization URL", Tags: []string{"oauth"},
		Parameters: []openapi.Parameter{{
			Name: "provider", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("DELETE", "/api/v1/oauth/{provider}", openapi.RouteMeta{
		Summary: "Disconnect an OAuth OBO provider", Tags: []string{"oauth"},
		Parameters: []openapi.Parameter{{
			Name: "provider", In: "path", Required: true,
			Schema: map[string]any{"type": "string"},
		}},
	})
	s.openapi.Register("POST", "/api/v1/oauth/token", openapi.RouteMeta{
		Summary: "Reveal a vault-backed OAuth OBO access token", Tags: []string{"oauth"},
	})
	s.openapi.Register("GET", "/oauth/callback/{provider}", openapi.RouteMeta{
		Summary: "OAuth OBO provider callback", Tags: []string{"oauth"},
	})
}

func writeOAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, oauth.ErrProviderUnknown):
		writeError(w, http.StatusNotFound, "OAUTH_PROVIDER_UNKNOWN", "OAuth provider is unknown", nil)
	case errors.Is(err, oauth.ErrProviderNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "OAUTH_PROVIDER_NOT_CONFIGURED",
			"OAuth provider is not configured on this runtime", nil)
	case errors.Is(err, oauth.ErrConnectionNotFound):
		writeError(w, http.StatusNotFound, "OAUTH_CONNECTION_NOT_FOUND", "OAuth connection not found", nil)
	case errors.Is(err, oauth.ErrInvalidState):
		writeError(w, http.StatusBadRequest, "OAUTH_INVALID_STATE", "OAuth state is invalid or expired", nil)
	case errors.Is(err, oauth.ErrInvalidReturnTo):
		writeError(w, http.StatusBadRequest, "OAUTH_INVALID_RETURN_TO", "OAuth return_to URL is not allowed", nil)
	case errors.Is(err, oauth.ErrUserRequired):
		writeError(w, http.StatusUnauthorized, "USER_REQUIRED", "user context required", nil)
	case errors.Is(err, oauth.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, oauth.ErrExchangeFailed):
		writeError(w, http.StatusBadGateway, "OAUTH_EXCHANGE_FAILED", "OAuth token exchange failed", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

func (s *Server) oauthUnavailable(w http.ResponseWriter) bool {
	if s.oauthManager == nil || s.oauthFactory == nil {
		writeError(w, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED",
			"OAuth-on-behalf-of-user is not configured on this runtime", nil)
		return true
	}
	return false
}

func (s *Server) handleOAuthProviders(w http.ResponseWriter, r *http.Request) {
	if s.oauthFactory == nil {
		writeJSON(w, http.StatusOK, oauthProviderListResponse{Providers: []oauth.ProviderInfo{}})
		return
	}
	// redirect_uri is request-scoped (it derives from AF_STACK_PUBLIC_URL or
	// the forwarded host), so the oauth package leaves it blank and we fill
	// it here. Operators need it to register the callback with the provider
	// console — populate it for every provider, configured or not.
	providers := s.oauthFactory.List()
	for i := range providers {
		providers[i].RedirectURI = oauthRedirectURI(r, providers[i].Name)
	}
	writeJSON(w, http.StatusOK, oauthProviderListResponse{Providers: providers})
}

func (s *Server) handleOAuthConnections(w http.ResponseWriter, r *http.Request) {
	if s.oauthUnavailable(w) {
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	userID, ok := oauthTargetUserID(w, r)
	if !ok {
		return
	}
	conns, err := s.oauthManager.List(r.Context(), tenantID, userID)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	if conns == nil {
		conns = []oauth.Connection{}
	}
	writeJSON(w, http.StatusOK, oauthConnectionListResponse{Connections: conns})
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if s.oauthUnavailable(w) {
		return
	}
	providerName := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	provider, err := s.oauthFactory.Get(providerName)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	userID := tenantctx.UserID(r.Context())
	if tenantID == "" {
		writeOAuthError(w, oauth.ErrTenantRequired)
		return
	}
	if userID == "" {
		writeOAuthError(w, oauth.ErrUserRequired)
		return
	}

	var in oauthAuthorizeInput
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &in); err != nil {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
				return
			}
		}
	}
	if err := oauth.ValidateReturnTo(in.ReturnTo, oauthAllowedReturnOrigins()); err != nil {
		writeOAuthError(w, err)
		return
	}
	state, err := oauth.SignState(oauthStateSecret(), oauth.State{
		TenantID: tenantID,
		UserID:   userID,
		Provider: providerName,
		ReturnTo: in.ReturnTo,
		IssuedAt: time.Now().Unix(),
		Nonce:    oauth.RandomNonce(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "OAUTH_STATE_SIGN_FAILED", err.Error(), nil)
		return
	}
	redirectURI := oauthRedirectURI(r, providerName)
	scopes := in.Scopes
	if len(scopes) == 0 {
		scopes = provider.DefaultScopes()
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "oauth.authorize",
		ResourceType: "oauth_connection",
		ResourceID:   providerName,
		Metadata: map[string]any{
			"provider":  providerName,
			"return_to": in.ReturnTo,
			"scopes":    scopes,
		},
	})
	writeJSON(w, http.StatusOK, oauthAuthorizeResponse{
		Provider: providerName, AuthorizationURL: provider.AuthorizeURL(state, scopes, redirectURI),
		RedirectURI: redirectURI, Scopes: scopes,
	})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauthUnavailable(w) {
		return
	}
	queryErr := strings.TrimSpace(r.URL.Query().Get("error"))
	if queryErr != "" {
		writeError(w, http.StatusBadRequest, "OAUTH_PROVIDER_DENIED", queryErr, nil)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "code is required", nil)
		return
	}
	state, err := oauth.VerifyState(oauthStateSecret(), strings.TrimSpace(r.URL.Query().Get("state")))
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	providerName := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	if providerName != state.Provider {
		writeOAuthError(w, oauth.ErrInvalidState)
		return
	}
	if err := oauth.ValidateReturnTo(state.ReturnTo, oauthAllowedReturnOrigins()); err != nil {
		writeOAuthError(w, err)
		return
	}
	provider, err := s.oauthFactory.Get(providerName)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	ts, err := provider.Exchange(r.Context(), code, oauthRedirectURI(r, providerName))
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	if len(ts.Scopes) == 0 {
		ts.Scopes = provider.DefaultScopes()
	}
	if err := s.oauthManager.StoreTokens(r.Context(), state.TenantID, state.UserID, providerName, ts); err != nil {
		writeOAuthError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "oauth.callback",
		ResourceType: "oauth_connection",
		ResourceID:   providerName,
		Metadata: map[string]any{
			"provider":  providerName,
			"tenant_id": state.TenantID,
			"user_id":   state.UserID,
			"scopes":    ts.Scopes,
		},
	})
	if state.ReturnTo != "" {
		// Tack a success marker onto the return so the customer-app can
		// toast / refetch without re-querying state.
		dest := state.ReturnTo
		if u, err := url.Parse(dest); err == nil {
			q := u.Query()
			q.Set("oauth", providerName)
			q.Set("status", "connected")
			u.RawQuery = q.Encode()
			dest = u.String()
		}
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connected": true,
		"provider":  providerName,
	})
}

func (s *Server) handleOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.oauthUnavailable(w) {
		return
	}
	providerName := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	provider, err := s.oauthFactory.Get(providerName)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	userID, ok := oauthTargetUserID(w, r)
	if !ok {
		return
	}
	if err := s.oauthManager.Disconnect(
		r.Context(), tenantID, userID, providerName, provider,
	); err != nil {
		writeOAuthError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "oauth.disconnect",
		ResourceType: "oauth_connection",
		ResourceID:   providerName,
		Metadata: map[string]any{
			"provider":  providerName,
			"tenant_id": tenantID,
			"user_id":   userID,
		},
	})
	writeJSON(w, http.StatusOK, map[string]bool{"disconnected": true})
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.oauthUnavailable(w) {
		return
	}
	// Internal-only: callers must set X-AF-Stack-Internal: 1. This
	// header is NOT in the CORS allow-list, so cross-origin browser
	// requests cannot legally send it; only server-side callers
	// (workload modules, agents, SDK) can.
	if r.Header.Get("X-AF-Stack-Internal") != "1" {
		writeError(w, http.StatusForbidden, "INTERNAL_ONLY",
			"this endpoint is internal-only; agents and workload modules call it via the SDK", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in oauthTokenInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	providerName := strings.ToLower(strings.TrimSpace(in.Provider))
	provider, err := s.oauthFactory.Get(providerName)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	tenantID := tenantctx.TenantID(r.Context())
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		userID = tenantctx.UserID(r.Context())
	} else if tenantctx.APIKeyID(r.Context()) == "" && tenantctx.UserID(r.Context()) != userID {
		writeError(w, http.StatusForbidden, "API_KEY_REQUIRED",
			"targeting another user_id requires an API key", nil)
		return
	}
	ts, err := s.oauthManager.Token(r.Context(), tenantID, userID, providerName, provider)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	var expiresAt *time.Time
	if !ts.ExpiresAt.IsZero() {
		t := ts.ExpiresAt
		expiresAt = &t
	}
	writeJSON(w, http.StatusOK, oauthTokenResponse{
		Provider: providerName, UserID: userID, AccessToken: ts.AccessToken,
		Scopes: ts.Scopes, ExpiresAt: expiresAt,
	})
}

func oauthTargetUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if raw := strings.TrimSpace(r.URL.Query().Get("user_id")); raw != "" {
		if tenantctx.APIKeyID(r.Context()) == "" {
			writeError(w, http.StatusForbidden, "API_KEY_REQUIRED",
				"targeting user_id requires an API key", nil)
			return "", false
		}
		return raw, true
	}
	userID := tenantctx.UserID(r.Context())
	if userID == "" {
		writeOAuthError(w, oauth.ErrUserRequired)
		return "", false
	}
	return userID, true
}

func oauthStateSecret() string {
	return strings.TrimSpace(os.Getenv("AF_STACK_AUTH_SECRET"))
}

func oauthAllowedReturnOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("AF_STACK_OAUTH_ALLOWED_RETURN_ORIGINS"))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func oauthRedirectURI(r *http.Request, provider string) string {
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
		if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			host = forwardedHost
		}
		base = scheme + "://" + host
	}
	return base + "/oauth/callback/" + url.PathEscape(provider)
}
