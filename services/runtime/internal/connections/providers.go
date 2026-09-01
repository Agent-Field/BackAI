// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"net/http"
	"strings"
)

// WebhookScheme names a provider's inbound-webhook signature algorithm.
type WebhookScheme string

const (
	// WebhookNone means the provider has no verifiable webhook signature.
	WebhookNone WebhookScheme = ""
	// WebhookStripe is Stripe's `Stripe-Signature: t=...,v1=<hmac>` scheme
	// (HMAC-SHA256 over "<timestamp>.<body>").
	WebhookStripe WebhookScheme = "stripe"
	// WebhookGitHubHMAC is GitHub's `X-Hub-Signature-256: sha256=<hmac>`
	// scheme (HMAC-SHA256 over the raw body).
	WebhookGitHubHMAC WebhookScheme = "github_hmac"
)

// Descriptor is the typed capability descriptor for one external provider.
// It is pure data + a couple of pure hooks: adding a provider means adding
// a Descriptor to the registry, never touching the trusted request/refresh
// core in service.go.
type Descriptor struct {
	// Name is the canonical lowercase provider id ("github", "stripe").
	Name string
	// Kind is the provider's native/primary auth kind (KindOAuth or
	// KindAPIKey). The create route still validates the caller's requested
	// kind against the descriptor's capabilities (OAuthCapable / api_key is
	// always allowed for a known provider).
	Kind string
	// BaseURL is the API root the HANDLE resolves relative paths against.
	BaseURL string
	// AuthHeaderName is the header the credential is injected under
	// (usually "Authorization").
	AuthHeaderName string
	// AuthValuePrefix is prepended to the token when building the header
	// value, e.g. "Bearer " (note the trailing space) or "token ".
	AuthValuePrefix string
	// AuthorizeEndpoint / TokenEndpoint are the OAuth consent + token URLs.
	// Empty for pure api_key providers.
	AuthorizeEndpoint string
	TokenEndpoint     string
	// DefaultScopes is requested when the caller doesn't override scopes.
	DefaultScopes []string
	// ScopeSeparator joins scopes in the authorize URL (" " for GitHub/
	// Google, "," for some providers).
	ScopeSeparator string
	// WebhookScheme is the inbound-webhook signature algorithm.
	WebhookScheme WebhookScheme
	// ClientIDEnv / ClientSecretEnv name the operator-configured platform
	// OAuth app credentials (resolved vault-first, then env, by the server).
	ClientIDEnv     string
	ClientSecretEnv string
	// requiredScope, when non-nil, maps an outbound (method, path) to the
	// scope the provider requires for it. Handle refuses the call when that
	// scope is not in the connection's granted scopes. nil disables
	// per-call scope enforcement (the default — most REST APIs don't expose
	// a static method/path→scope map, so enforcement stays honest rather
	// than fabricated).
	requiredScope func(method, path string) string
}

// OAuthCapable reports whether the descriptor can run an OAuth flow.
func (d Descriptor) OAuthCapable() bool {
	return d.AuthorizeEndpoint != "" && d.TokenEndpoint != ""
}

// injectAuth stamps the credential onto an outbound request under the
// descriptor's auth header. token is the API key (api_key kind) or the live
// access token (oauth kind). Any caller-supplied value under the same header
// is overwritten so app code can never smuggle its own credential in.
func (d Descriptor) injectAuth(req *http.Request, token string) {
	name := d.AuthHeaderName
	if name == "" {
		name = "Authorization"
	}
	req.Header.Set(name, d.AuthValuePrefix+token)
}

// authHeaderName returns the effective auth header name (default
// "Authorization").
func (d Descriptor) authHeaderName() string {
	if d.AuthHeaderName == "" {
		return "Authorization"
	}
	return d.AuthHeaderName
}

// requiredScopeFor returns the scope the provider requires for a given
// outbound call, or "" when no enforcement applies.
func (d Descriptor) requiredScopeFor(method, path string) string {
	if d.requiredScope == nil {
		return ""
	}
	return d.requiredScope(method, path)
}

// Registry is the in-code provider inventory. It is immutable after
// construction and safe for concurrent reads.
type Registry struct {
	byName map[string]Descriptor
}

// DefaultRegistry returns the built-in provider set: github, stripe,
// google, slack.
func DefaultRegistry() *Registry {
	r := &Registry{byName: map[string]Descriptor{}}
	for _, d := range defaultDescriptors() {
		r.byName[d.Name] = d
	}
	return r
}

// Get returns the descriptor for a provider name (case-insensitive).
// Returns ErrUnknownProvider when unregistered.
func (r *Registry) Get(name string) (Descriptor, error) {
	d, ok := r.byName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Descriptor{}, ErrUnknownProvider
	}
	return d, nil
}

// Names returns the registered provider names in a stable order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	// small n; insertion sort keeps it dependency-free and deterministic.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func defaultDescriptors() []Descriptor {
	return []Descriptor{
		{
			Name:              "github",
			Kind:              KindOAuth,
			BaseURL:           "https://api.github.com",
			AuthHeaderName:    "Authorization",
			AuthValuePrefix:   "Bearer ",
			AuthorizeEndpoint: "https://github.com/login/oauth/authorize",
			TokenEndpoint:     "https://github.com/login/oauth/access_token",
			DefaultScopes:     []string{"repo", "read:user"},
			ScopeSeparator:    " ",
			WebhookScheme:     WebhookGitHubHMAC,
			ClientIDEnv:       "CONNECTIONS_GITHUB_CLIENT_ID",
			ClientSecretEnv:   "CONNECTIONS_GITHUB_CLIENT_SECRET",
		},
		{
			Name:            "stripe",
			Kind:            KindAPIKey,
			BaseURL:         "https://api.stripe.com",
			AuthHeaderName:  "Authorization",
			AuthValuePrefix: "Bearer ",
			WebhookScheme:   WebhookStripe,
			// Stripe is a secret-key provider — no OAuth consent flow in the
			// standard integration path, so no authorize/token endpoints.
		},
		{
			Name:              "google",
			Kind:              KindOAuth,
			BaseURL:           "https://www.googleapis.com",
			AuthHeaderName:    "Authorization",
			AuthValuePrefix:   "Bearer ",
			AuthorizeEndpoint: "https://accounts.google.com/o/oauth2/v2/auth",
			TokenEndpoint:     "https://oauth2.googleapis.com/token",
			DefaultScopes:     []string{"https://www.googleapis.com/auth/userinfo.email"},
			ScopeSeparator:    " ",
			WebhookScheme:     WebhookNone,
			ClientIDEnv:       "CONNECTIONS_GOOGLE_CLIENT_ID",
			ClientSecretEnv:   "CONNECTIONS_GOOGLE_CLIENT_SECRET",
		},
		{
			Name:              "slack",
			Kind:              KindOAuth,
			BaseURL:           "https://slack.com/api",
			AuthHeaderName:    "Authorization",
			AuthValuePrefix:   "Bearer ",
			AuthorizeEndpoint: "https://slack.com/oauth/v2/authorize",
			TokenEndpoint:     "https://slack.com/api/oauth.v2.access",
			DefaultScopes:     []string{"chat:write"},
			ScopeSeparator:    ",",
			WebhookScheme:     WebhookNone,
			ClientIDEnv:       "CONNECTIONS_SLACK_CLIENT_ID",
			ClientSecretEnv:   "CONNECTIONS_SLACK_CLIENT_SECRET",
		},
	}
}
