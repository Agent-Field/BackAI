// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/adapters/github"
	"github.com/Agent-Field/backai/services/runtime/internal/oauth/adapters/google"
)

// AllProviderNames is the canonical ordering used by the dashboard for
// rendering the integrations page. Adding a new provider means adding a
// new adapter package and appending here.
var AllProviderNames = []string{
	"google",
	"github",
}

// ProviderInfo is the dashboard-facing view of a provider: whether it is
// configured, where its credentials come from, the callback redirect URI
// to register with the provider, and its default scopes. Tokens /
// connection state are reported separately via Manager.List.
type ProviderInfo struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	// CredentialsSource reports where the resolved client id/secret came
	// from: "vault" (operator-entered integration slot), "env"
	// (OAUTH_<NAME>_* environment variables), or "" when unconfigured.
	CredentialsSource string `json:"credentials_source"`
	// RedirectURI is the provider callback URL the operator must register
	// with the third-party console. It is request-scoped, so the oauth
	// package leaves it empty; the server layer populates it in
	// handleOAuthProviders from the request host / AF_STACK_PUBLIC_URL.
	RedirectURI   string   `json:"redirect_uri"`
	DefaultScopes []string `json:"default_scopes"`
}

// CredentialResolver resolves an operator-entered credential field for a
// provider (e.g. provider="google", field="client_id"). It returns the
// value and whether it was found. The server layer injects a resolver
// backed by the per-tenant secrets vault (integration slots); the oauth
// package never imports the server package. A nil resolver means
// vault-backed credentials are unavailable and only env is consulted.
type CredentialResolver func(provider, field string) (value string, ok bool)

// cachedAdapter memoises a constructed Provider keyed by the exact
// credentials it was built from. When resolved credentials change (an
// operator saves a new client id/secret) the cache miss rebuilds the
// adapter, so a credential update takes effect without a restart.
type cachedAdapter struct {
	id, secret string
	provider   Provider
}

// Factory owns provider construction. Adapters are constructed lazily and
// resolved vault-first (operator-entered integration slots) with an env
// fallback (OAUTH_<NAME>_CLIENT_ID / OAUTH_<NAME>_CLIENT_SECRET). A
// credential saved via the admin API takes effect on the next Get/List
// with no runtime restart.
type Factory struct {
	httpClient *http.Client
	// resolver resolves vault-backed credentials; nil ⇒ env-only.
	resolver CredentialResolver
	// static holds explicitly-injected providers (NewFactory). These
	// bypass dynamic resolution and are always reported configured.
	static map[string]Provider

	mu    sync.Mutex
	cache map[string]cachedAdapter
}

// newFactory is the shared constructor.
func newFactory() *Factory {
	return &Factory{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		static:     map[string]Provider{},
		cache:      map[string]cachedAdapter{},
	}
}

// NewFactoryFromEnv constructs a Factory that resolves providers
// dynamically. With no credential resolver wired it consults only the
// environment (OAUTH_<NAME>_CLIENT_ID / OAUTH_<NAME>_CLIENT_SECRET);
// wire a resolver with SetCredentialResolver to make operator-entered
// vault credentials take precedence.
//
// The same http.Client is shared across adapters. A 30-second timeout is
// generous for OAuth token exchanges (which include a TCP+TLS handshake
// to the provider) and tight enough that a hung provider doesn't pin
// goroutines forever.
func NewFactoryFromEnv() *Factory {
	return newFactory()
}

// NewFactory constructs a Factory from explicit providers. Tests and
// embedded runtimes use this to inject fake providers without relying on
// process env or the vault. Unknown provider names are ignored.
func NewFactory(providers map[string]Provider) *Factory {
	f := newFactory()
	for name, p := range providers {
		name = strings.ToLower(strings.TrimSpace(name))
		if p != nil && knownProvider(name) {
			f.static[name] = p
		}
	}
	return f
}

// SetCredentialResolver wires a vault-backed credential resolver. Called
// once from server wiring after the secrets vault is available. Passing
// nil reverts to env-only resolution.
func (f *Factory) SetCredentialResolver(r CredentialResolver) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolver = r
	// A changed resolver invalidates any memoised adapters; the next
	// Get/List rebuilds from freshly resolved credentials.
	f.cache = map[string]cachedAdapter{}
}

// readClient pulls (OAUTH_<name>_CLIENT_ID, OAUTH_<name>_CLIENT_SECRET)
// from the environment. The OAUTH_ prefix disambiguates from better-auth's
// GOOGLE_CLIENT_ID (which is for SIGN-IN, not agent-on-behalf-of-user).
func readClient(name string) (id, secret string, ok bool) {
	id = strings.TrimSpace(os.Getenv("OAUTH_" + name + "_CLIENT_ID"))
	secret = strings.TrimSpace(os.Getenv("OAUTH_" + name + "_CLIENT_SECRET"))
	return id, secret, id != "" && secret != ""
}

// resolveCreds resolves (client_id, client_secret) for a provider,
// vault-first then env. source is "vault", "env", or "" when neither
// yields a complete credential pair.
func (f *Factory) resolveCreds(name string) (id, secret, source string) {
	f.mu.Lock()
	resolver := f.resolver
	f.mu.Unlock()

	if resolver != nil {
		vid, idOK := resolver(name, "client_id")
		vsec, secOK := resolver(name, "client_secret")
		vid = strings.TrimSpace(vid)
		vsec = strings.TrimSpace(vsec)
		if idOK && secOK && vid != "" && vsec != "" {
			return vid, vsec, "vault"
		}
	}
	if eid, esec, ok := readClient(strings.ToUpper(name)); ok {
		return eid, esec, "env"
	}
	return "", "", ""
}

// providerFor returns a Provider for name built from the given
// credentials, reusing a memoised adapter when the credentials are
// unchanged. Callers must have already validated name via knownProvider.
func (f *Factory) providerFor(name, id, secret string) Provider {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.cache[name]; ok && c.id == id && c.secret == secret {
		return c.provider
	}
	p := buildAdapter(name, id, secret, f.httpClient)
	if p == nil {
		return nil
	}
	f.cache[name] = cachedAdapter{id: id, secret: secret, provider: p}
	return p
}

// buildAdapter constructs the concrete adapter for a known provider.
func buildAdapter(name, id, secret string, client *http.Client) Provider {
	switch name {
	case "google":
		return google.New(id, secret, client)
	case "github":
		return github.New(id, secret, client)
	default:
		return nil
	}
}

// Get returns the Provider for name. Returns ErrProviderUnknown when
// name isn't a known provider, ErrProviderNotConfigured when it's known
// but no credentials resolve (neither vault nor env). Credentials are
// resolved on every call, so an operator-entered credential takes effect
// on the next request without a restart.
func (f *Factory) Get(name string) (Provider, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !knownProvider(name) {
		return nil, ErrProviderUnknown
	}
	f.mu.Lock()
	p, ok := f.static[name]
	f.mu.Unlock()
	if ok {
		return p, nil
	}
	id, secret, source := f.resolveCreds(name)
	if source == "" {
		return nil, ErrProviderNotConfigured
	}
	if p := f.providerFor(name, id, secret); p != nil {
		return p, nil
	}
	return nil, ErrProviderNotConfigured
}

// IsConfigured reports whether the provider currently resolves a complete
// credential pair (vault or env) or was explicitly injected.
func (f *Factory) IsConfigured(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	f.mu.Lock()
	_, ok := f.static[name]
	f.mu.Unlock()
	if ok {
		return true
	}
	_, _, source := f.resolveCreds(name)
	return source != ""
}

// List returns ProviderInfo for every known provider, resolving
// credentials vault-first with env fallback. RedirectURI is left empty —
// it is request-scoped and populated by the server layer.
func (f *Factory) List() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(AllProviderNames))
	for _, name := range AllProviderNames {
		info := ProviderInfo{Name: name}
		f.mu.Lock()
		p, ok := f.static[name]
		f.mu.Unlock()
		if ok {
			info.Configured = true
			info.CredentialsSource = "env"
			info.DefaultScopes = p.DefaultScopes()
			out = append(out, info)
			continue
		}
		id, secret, source := f.resolveCreds(name)
		if source != "" {
			if adapter := f.providerFor(name, id, secret); adapter != nil {
				info.Configured = true
				info.CredentialsSource = source
				info.DefaultScopes = adapter.DefaultScopes()
				out = append(out, info)
				continue
			}
		}
		info.DefaultScopes = defaultScopesFor(name)
		out = append(out, info)
	}
	return out
}

// knownProvider gates URL-parameter values so a 404 falls through to
// ErrProviderUnknown rather than a misleading "not configured".
func knownProvider(name string) bool {
	for _, n := range AllProviderNames {
		if n == name {
			return true
		}
	}
	return false
}

// defaultScopesFor returns the scope set a provider WOULD ask for if it
// were configured. Used by ProviderInfo so the dashboard can still
// preview scopes for unconfigured providers.
func defaultScopesFor(name string) []string {
	switch name {
	case "google":
		return google.DefaultScopes
	case "github":
		return github.DefaultScopes
	default:
		return nil
	}
}
