// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// refreshSkew is the window before token_expiry at which Handle proactively
// refreshes an OAuth access token. Matches the login-OAuth manager.
const refreshSkew = 60 * time.Second

// maxProviderBody caps how much of a provider response we read into memory.
const maxProviderBody = 16 << 20

// ClientCreds is a provider's operator-configured platform OAuth app
// credential (the client_id/client_secret of the *runtime's* registered app,
// distinct from any per-tenant token).
type ClientCreds struct {
	ClientID     string
	ClientSecret string
}

// ClientCredsResolver resolves platform OAuth client creds for a provider.
// The server wires an implementation that reads vault-first then env; tests
// inject a fake. ok=false means the provider isn't configured for OAuth on
// this runtime.
type ClientCredsResolver interface {
	ClientCreds(ctx context.Context, provider string) (creds ClientCreds, ok bool)
}

// ClientCredsFunc adapts a function to ClientCredsResolver.
type ClientCredsFunc func(ctx context.Context, provider string) (ClientCreds, bool)

// ClientCreds implements ClientCredsResolver.
func (f ClientCredsFunc) ClientCreds(ctx context.Context, provider string) (ClientCreds, bool) {
	return f(ctx, provider)
}

// HandleRequest is what app code sends to the HANDLE. The app never sees a
// credential — it names a method/path and the runtime injects auth.
type HandleRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"-"`
}

// HandleResult is the provider's response, relayed verbatim to app code.
type HandleResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"-"`
}

// Service is the connections subsystem entrypoint. Construct one per process.
type Service struct {
	store       Store
	registry    *Registry
	httpClient  *http.Client
	clientCreds ClientCredsResolver
	stateSecret string
	log         *slog.Logger
	refresh     *refreshGroup
	now         func() time.Time
}

// Config configures a Service. Store, HTTPClient, and Registry are required
// (New fills a DefaultRegistry when nil). Now defaults to time.Now.
type Config struct {
	Store       Store
	Registry    *Registry
	HTTPClient  *http.Client
	ClientCreds ClientCredsResolver
	StateSecret string
	Log         *slog.Logger
	Now         func() time.Time
}

// New constructs a Service.
func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("connections: store required")
	}
	if cfg.HTTPClient == nil {
		return nil, errors.New("connections: http client required")
	}
	reg := cfg.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:       cfg.Store,
		registry:    reg,
		httpClient:  cfg.HTTPClient,
		clientCreds: cfg.ClientCreds,
		stateSecret: cfg.StateSecret,
		log:         log,
		refresh:     &refreshGroup{},
		now:         now,
	}, nil
}

// Registry exposes the provider registry for read-only listing.
func (s *Service) Registry() *Registry { return s.registry }

// CreateAPIKeyParams is the input to CreateAPIKey.
type CreateAPIKeyParams struct {
	Provider      string
	Name          string
	APIKey        string
	Scopes        []string
	WebhookSecret string
	CreatedBy     string
}

// CreateAPIKey stores an api_key connection. The credential is encrypted and
// is never returned. A "created" audit event is emitted.
func (s *Service) CreateAPIKey(ctx context.Context, tenantID string, p CreateAPIKeyParams) (Connection, error) {
	desc, err := s.registry.Get(p.Provider)
	if err != nil {
		return Connection{}, err
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return Connection{}, ErrCredentialRequired
	}
	conn, err := s.store.Create(ctx, tenantID, CreateParams{
		Provider:        desc.Name,
		Kind:            KindAPIKey,
		Name:            p.Name,
		Cred:            credential{APIKey: p.APIKey},
		RequestedScopes: p.Scopes,
		GrantedScopes:   p.Scopes,
		WebhookSecret:   p.WebhookSecret,
		CreatedBy:       p.CreatedBy,
	})
	if err != nil {
		return Connection{}, err
	}
	s.emit(ctx, tenantID, conn.ID, "created", map[string]any{
		"provider": desc.Name, "kind": KindAPIKey,
	})
	return conn.withHealth(s.now()), nil
}

// AuthorizeParams is the input to AuthorizeURL.
type AuthorizeParams struct {
	Provider    string
	Name        string
	Scopes      []string
	ReturnTo    string
	CreatedBy   string
	RedirectURI string
}

// AuthorizeURL begins an OAuth connection: it returns the provider consent
// URL and the signed state to round-trip. No connection row is created until
// the consent completes at CompleteOAuth. Returns ErrKindUnsupported when
// the provider has no OAuth flow, ErrProviderNotConfigured when the platform
// client creds are missing.
func (s *Service) AuthorizeURL(ctx context.Context, tenantID string, p AuthorizeParams) (authorizeURL, state string, err error) {
	desc, err := s.registry.Get(p.Provider)
	if err != nil {
		return "", "", err
	}
	if !desc.OAuthCapable() {
		return "", "", ErrKindUnsupported
	}
	creds, ok := s.resolveClientCreds(ctx, desc.Name)
	if !ok {
		return "", "", ErrProviderNotConfigured
	}
	scopes := p.Scopes
	if len(scopes) == 0 {
		scopes = desc.DefaultScopes
	}
	st, err := signState(s.stateSecret, oauthState{
		Tenant:    tenantID,
		Provider:  desc.Name,
		Name:      p.Name,
		Scopes:    scopes,
		ReturnTo:  p.ReturnTo,
		CreatedBy: p.CreatedBy,
		IssuedAt:  s.now().Unix(),
		Nonce:     randomNonce(),
	})
	if err != nil {
		return "", "", err
	}
	return desc.buildAuthorizeURL(creds.ClientID, st, scopes, p.RedirectURI), st, nil
}

// buildAuthorizeURL constructs the provider consent URL.
func (d Descriptor) buildAuthorizeURL(clientID, state string, scopes []string, redirectURI string) string {
	sep := d.ScopeSeparator
	if sep == "" {
		sep = " "
	}
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, sep))
	}
	joiner := "?"
	if strings.Contains(d.AuthorizeEndpoint, "?") {
		joiner = "&"
	}
	return d.AuthorizeEndpoint + joiner + q.Encode()
}

// CompleteOAuth finishes the consent round-trip: it verifies the state,
// exchanges the code for tokens using the platform client creds, and creates
// the connection row. The tenant comes from the verified state, never from
// the request. Returns the created Connection (credential-free).
func (s *Service) CompleteOAuth(ctx context.Context, provider, code, state, redirectURI string) (Connection, error) {
	st, err := verifyState(s.stateSecret, state)
	if err != nil {
		return Connection{}, err
	}
	if !strings.EqualFold(st.Provider, strings.TrimSpace(provider)) {
		return Connection{}, ErrInvalidState
	}
	desc, err := s.registry.Get(st.Provider)
	if err != nil {
		return Connection{}, err
	}
	creds, ok := s.resolveClientCreds(ctx, desc.Name)
	if !ok {
		return Connection{}, ErrProviderNotConfigured
	}
	tok, err := s.exchangeCode(ctx, desc, creds, code, redirectURI)
	if err != nil {
		return Connection{}, err
	}
	granted := tok.scopes
	if len(granted) == 0 {
		granted = st.Scopes
	}
	conn, err := s.store.Create(ctx, st.Tenant, CreateParams{
		Provider:        desc.Name,
		Kind:            KindOAuth,
		Name:            st.Name,
		Cred:            tok.cred,
		RequestedScopes: st.Scopes,
		GrantedScopes:   granted,
		TokenExpiry:     tok.expiry,
		CreatedBy:       st.CreatedBy,
	})
	if err != nil {
		return Connection{}, err
	}
	s.emit(ctx, st.Tenant, conn.ID, "created", map[string]any{
		"provider": desc.Name, "kind": KindOAuth, "scopes": granted,
	})
	return conn.withHealth(s.now()), nil
}

// List returns the tenant's connections with a derived health label. Never
// materialises credentials.
func (s *Service) List(ctx context.Context, tenantID string) ([]Connection, error) {
	conns, err := s.store.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]Connection, 0, len(conns))
	for _, c := range conns {
		out = append(out, c.withHealth(now))
	}
	return out, nil
}

// Revoke marks a connection revoked and best-effort revokes the token with
// the provider. Provider-side failures are logged, never fatal — the local
// revocation is what the caller asked for. Emits a "revoked" audit event.
func (s *Service) Revoke(ctx context.Context, tenantID, id string) error {
	loaded, err := s.store.Load(ctx, tenantID, id)
	if err != nil {
		return err
	}
	desc, dErr := s.registry.Get(loaded.Conn.Provider)
	if dErr == nil {
		s.bestEffortProviderRevoke(ctx, desc, loaded)
	}
	if err := s.store.UpdateStatus(ctx, tenantID, id, StatusRevoked); err != nil {
		return err
	}
	s.emit(ctx, tenantID, id, "revoked", map[string]any{"provider": loaded.Conn.Provider})
	return nil
}

// Handle is the credential-injecting proxy. It loads the connection, refreshes
// an expired OAuth token (single-flight), enforces granted scopes where the
// provider exposes a mapping, injects the credential server-side, and returns
// the provider's response. App code never sees the credential.
func (s *Service) Handle(ctx context.Context, tenantID, id string, req HandleRequest) (HandleResult, error) {
	loaded, err := s.store.Load(ctx, tenantID, id)
	if err != nil {
		return HandleResult{}, err
	}
	if loaded.Conn.Status == StatusRevoked {
		return HandleResult{}, ErrRevoked
	}
	desc, err := s.registry.Get(loaded.Conn.Provider)
	if err != nil {
		return HandleResult{}, err
	}

	token, err := s.liveToken(ctx, tenantID, desc, loaded)
	if err != nil {
		return HandleResult{}, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if scope := desc.requiredScopeFor(method, req.Path); scope != "" && !containsScope(loaded.Conn.GrantedScopes, scope) {
		return HandleResult{}, ErrScopeNotGranted
	}

	target, err := desc.resolveURL(req.Path, req.Query)
	if err != nil {
		return HandleResult{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = strings.NewReader(string(req.Body))
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return HandleResult{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	applyForwardHeaders(httpReq, req.Headers, desc.authHeaderName())
	desc.injectAuth(httpReq, token)

	// Log the outbound request with the auth header REDACTED and no header
	// values at all — the token must never reach logs, spans, or errors.
	s.log.Info("connections: proxying request",
		"connection_id", id,
		"provider", desc.Name,
		"method", method,
		"url", target,
		"auth_header", desc.authHeaderName()+": [REDACTED]",
		"header_keys", forwardedHeaderKeys(req.Headers, desc.authHeaderName()),
	)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.emit(ctx, tenantID, id, "error", map[string]any{
			"provider": desc.Name, "reason": "transport", "error": redactErr(err, token),
		})
		return HandleResult{}, fmt.Errorf("connections: provider request failed: %v", redactErr(err, token))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBody))
	if err != nil {
		return HandleResult{}, fmt.Errorf("connections: read provider response: %w", err)
	}

	if resp.StatusCode >= 500 {
		s.emit(ctx, tenantID, id, "error", map[string]any{
			"provider": desc.Name, "status": resp.StatusCode,
		})
	} else {
		s.emit(ctx, tenantID, id, "health_check", map[string]any{
			"provider": desc.Name, "status": resp.StatusCode,
		})
	}

	return HandleResult{
		Status:  resp.StatusCode,
		Headers: responseHeaders(resp.Header),
		Body:    respBody,
	}, nil
}

// VerifyWebhook checks a provider webhook signature against the connection's
// stored signing secret. headers is the raw inbound header set; the relevant
// signature header is selected by the provider's scheme.
func (s *Service) VerifyWebhook(ctx context.Context, tenantID, id string, headers map[string]string, body []byte) (bool, error) {
	loaded, err := s.store.Load(ctx, tenantID, id)
	if err != nil {
		return false, err
	}
	desc, err := s.registry.Get(loaded.Conn.Provider)
	if err != nil {
		return false, err
	}
	sigHeader := selectSignatureHeader(desc.WebhookScheme, headers)
	valid, err := VerifyWebhookSignature(desc.WebhookScheme, loaded.webhookSecret, sigHeader, body, s.now())
	if err != nil {
		return false, err
	}
	s.emit(ctx, tenantID, id, "health_check", map[string]any{
		"provider": desc.Name, "webhook_verified": valid,
	})
	return valid, nil
}

// liveToken returns the credential value to inject, refreshing an expired
// OAuth token via single-flight when needed.
func (s *Service) liveToken(ctx context.Context, tenantID string, desc Descriptor, loaded Loaded) (string, error) {
	conn := loaded.Conn
	if conn.Kind == KindAPIKey {
		if loaded.cred.APIKey == "" {
			return "", ErrCredentialRequired
		}
		return loaded.cred.APIKey, nil
	}

	// oauth kind.
	if conn.TokenExpiry != nil && !s.now().Add(refreshSkew).Before(*conn.TokenExpiry) {
		if loaded.cred.RefreshToken == "" {
			// Expired and nothing to refresh with — surface the stale token so
			// the caller gets the provider's 401 and can reconnect.
			if loaded.cred.AccessToken == "" {
				return "", ErrRefreshFailed
			}
			return loaded.cred.AccessToken, nil
		}
		res, _ := s.refresh.do(conn.ID, func() refreshResult {
			return s.doRefresh(ctx, tenantID, desc, loaded)
		})
		if res.err != nil {
			return "", res.err
		}
		return res.cred.AccessToken, nil
	}
	if loaded.cred.AccessToken == "" {
		return "", ErrRefreshFailed
	}
	return loaded.cred.AccessToken, nil
}

// doRefresh performs the actual token refresh + persistence. It runs at most
// once per connection id per expiry window thanks to refreshGroup.
func (s *Service) doRefresh(ctx context.Context, tenantID string, desc Descriptor, loaded Loaded) refreshResult {
	creds, ok := s.resolveClientCreds(ctx, desc.Name)
	if !ok {
		return refreshResult{err: ErrProviderNotConfigured}
	}
	tok, err := s.refreshToken(ctx, desc, creds, loaded.cred.RefreshToken)
	if err != nil {
		s.emit(ctx, tenantID, loaded.Conn.ID, "error", map[string]any{
			"provider": desc.Name, "reason": "refresh",
		})
		// Best-effort: mark the connection errored so health reflects it.
		_ = s.store.UpdateStatus(ctx, tenantID, loaded.Conn.ID, StatusError)
		return refreshResult{err: err}
	}
	// Providers rotate refresh tokens inconsistently; keep the old one when
	// the response omits a new one.
	if tok.cred.RefreshToken == "" {
		tok.cred.RefreshToken = loaded.cred.RefreshToken
	}
	granted := tok.scopes
	if len(granted) == 0 {
		granted = loaded.Conn.GrantedScopes
	}
	if err := s.store.SaveTokens(ctx, tenantID, loaded.Conn.ID, tok.cred, tok.expiry, granted); err != nil {
		return refreshResult{err: err}
	}
	s.emit(ctx, tenantID, loaded.Conn.ID, "refreshed", map[string]any{"provider": desc.Name})
	return refreshResult{cred: tok.cred}
}

func (s *Service) resolveClientCreds(ctx context.Context, provider string) (ClientCreds, bool) {
	if s.clientCreds == nil {
		return ClientCreds{}, false
	}
	return s.clientCreds.ClientCreds(ctx, provider)
}

func (s *Service) emit(ctx context.Context, tenantID, connID, eventType string, metadata map[string]any) {
	if err := s.store.InsertEvent(ctx, tenantID, connID, eventType, metadata); err != nil {
		s.log.Warn("connections: audit event write failed",
			"connection_id", connID, "event_type", eventType, "error", err)
	}
}

func (s *Service) bestEffortProviderRevoke(ctx context.Context, desc Descriptor, loaded Loaded) {
	// Only OAuth tokens have a provider-side revoke concept in the built-in
	// set, and the endpoints are provider-specific. We attempt a token revoke
	// only where a concrete endpoint is known; otherwise the local status
	// flip is the revocation. Failures are logged, never returned.
	endpoint := desc.revokeEndpoint()
	if endpoint == "" || loaded.cred.AccessToken == "" {
		return
	}
	form := url.Values{}
	form.Set("token", loaded.cred.AccessToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		s.log.Warn("connections: provider revoke failed", "provider", desc.Name, "error", redactErr(err, loaded.cred.AccessToken))
		return
	}
	_ = resp.Body.Close()
}

// containsScope reports whether granted includes scope.
func containsScope(granted []string, scope string) bool {
	for _, g := range granted {
		if g == scope {
			return true
		}
	}
	return false
}

// resolveURL joins the provider base URL with the app-supplied path + query.
// The path is treated as relative to BaseURL; an absolute URL in path is
// rejected so app code can't retarget the injected credential at an arbitrary
// host.
func (d Descriptor) resolveURL(path string, query map[string]string) (string, error) {
	base, err := url.Parse(d.BaseURL)
	if err != nil {
		return "", fmt.Errorf("bad base url: %w", err)
	}
	p := strings.TrimSpace(path)
	if p == "" {
		p = "/"
	}
	// Reject scheme/host in the path — credential retargeting guard.
	if strings.Contains(p, "://") || strings.HasPrefix(p, "//") {
		return "", errors.New("path must be relative to the provider base url")
	}
	rel, err := url.Parse(p)
	if err != nil {
		return "", fmt.Errorf("bad path: %w", err)
	}
	if rel.Host != "" || rel.Scheme != "" {
		return "", errors.New("path must be relative to the provider base url")
	}
	resolved := base.ResolveReference(&url.URL{Path: rel.Path, RawQuery: rel.RawQuery})
	// Force the host back to the descriptor's — ResolveReference keeps base
	// host for a relative ref, but be defensive.
	resolved.Scheme = base.Scheme
	resolved.Host = base.Host
	if len(query) > 0 {
		q := resolved.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		resolved.RawQuery = q.Encode()
	}
	return resolved.String(), nil
}

func (d Descriptor) revokeEndpoint() string {
	switch d.Name {
	case "google":
		return "https://oauth2.googleapis.com/revoke"
	case "github":
		// GitHub token revocation is an authenticated DELETE on the app's
		// grant endpoint (needs basic auth with client creds) — not a simple
		// token POST, so we skip it here and rely on the local status flip.
		return ""
	default:
		return ""
	}
}

// hopByHopHeaders are never forwarded from the app request to the provider.
var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
	"content-length":      {},
}

// applyForwardHeaders copies the app-supplied header subset onto the outbound
// request, dropping hop-by-hop headers and any attempt to set the auth header
// (which the runtime owns).
func applyForwardHeaders(req *http.Request, headers map[string]string, authHeaderName string) {
	authLower := strings.ToLower(authHeaderName)
	for k, v := range headers {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" || lk == authLower {
			continue
		}
		if _, hop := hopByHopHeaders[lk]; hop {
			continue
		}
		req.Header.Set(k, v)
	}
}

// forwardedHeaderKeys returns the sorted header keys the app asked to forward
// (excluding the auth header), for log context — values are never logged.
func forwardedHeaderKeys(headers map[string]string, authHeaderName string) []string {
	authLower := strings.ToLower(authHeaderName)
	out := make([]string, 0, len(headers))
	for k := range headers {
		if strings.ToLower(strings.TrimSpace(k)) == authLower {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// responseHeaders flattens a small, safe subset of provider response headers
// for relay to app code.
func responseHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"Content-Type", "X-Request-Id", "X-Ratelimit-Remaining", "X-Ratelimit-Limit", "Retry-After"} {
		if v := h.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// selectSignatureHeader picks the provider's signature header value from a
// case-insensitive header map.
func selectSignatureHeader(scheme WebhookScheme, headers map[string]string) string {
	var want string
	switch scheme {
	case WebhookStripe:
		want = "stripe-signature"
	case WebhookGitHubHMAC:
		want = "x-hub-signature-256"
	case WebhookNone:
		return ""
	default:
		return ""
	}
	for k, v := range headers {
		if strings.EqualFold(strings.TrimSpace(k), want) {
			return v
		}
	}
	return ""
}

// redactErr scrubs a token substring out of an error string so a transport
// error that echoes the URL/headers can't leak the credential.
func redactErr(err error, token string) string {
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "[REDACTED]")
	}
	return msg
}
