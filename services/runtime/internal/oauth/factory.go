// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"net/http"
	"os"
	"strings"
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

// ProviderInfo is the dashboard-facing view of a provider: configured
// (env keys present) + default scopes. Tokens / connection state are
// reported separately via Manager.List.
type ProviderInfo struct {
	Name          string   `json:"name"`
	Configured    bool     `json:"configured"`
	DefaultScopes []string `json:"default_scopes"`
}

// Factory owns provider construction. Adapters are constructed lazily
// based on env-driven config; one adapter per provider when both
// OAUTH_<NAME>_CLIENT_ID and OAUTH_<NAME>_CLIENT_SECRET are set.
type Factory struct {
	httpClient *http.Client
	providers  map[string]Provider
}

// NewFactoryFromEnv inspects the environment and constructs a Factory
// with every configured provider wired up. Unconfigured providers are
// silently skipped — callers detect this via Get returning
// ErrProviderNotConfigured.
//
// The same http.Client is shared across adapters. A 30-second timeout is
// generous for OAuth token exchanges (which include a TCP+TLS handshake
// to the provider) and tight enough that a hung provider doesn't pin
// goroutines forever.
func NewFactoryFromEnv() *Factory {
	client := &http.Client{Timeout: 30 * time.Second}
	f := &Factory{
		httpClient: client,
		providers:  map[string]Provider{},
	}

	if id, secret, ok := readClient("GOOGLE"); ok {
		f.providers["google"] = google.New(id, secret, client)
	}
	if id, secret, ok := readClient("GITHUB"); ok {
		f.providers["github"] = github.New(id, secret, client)
	}
	return f
}

// NewFactory constructs a Factory from explicit providers. Tests and
// embedded runtimes use this to inject fake providers without relying on
// process env. Unknown provider names are ignored.
func NewFactory(providers map[string]Provider) *Factory {
	f := &Factory{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		providers:  map[string]Provider{},
	}
	for name, p := range providers {
		name = strings.ToLower(strings.TrimSpace(name))
		if p != nil && knownProvider(name) {
			f.providers[name] = p
		}
	}
	return f
}

// readClient pulls (OAUTH_<name>_CLIENT_ID, OAUTH_<name>_CLIENT_SECRET)
// from the environment. The OAUTH_ prefix disambiguates from better-auth's
// GOOGLE_CLIENT_ID (which is for SIGN-IN, not agent-on-behalf-of-user).
func readClient(name string) (id, secret string, ok bool) {
	id = strings.TrimSpace(os.Getenv("OAUTH_" + name + "_CLIENT_ID"))
	secret = strings.TrimSpace(os.Getenv("OAUTH_" + name + "_CLIENT_SECRET"))
	return id, secret, id != "" && secret != ""
}

// Get returns the Provider for name. Returns ErrProviderUnknown when
// name isn't a known provider, ErrProviderNotConfigured when it's
// known but the env vars are missing.
func (f *Factory) Get(name string) (Provider, error) {
	if !knownProvider(name) {
		return nil, ErrProviderUnknown
	}
	if p, ok := f.providers[name]; ok {
		return p, nil
	}
	return nil, ErrProviderNotConfigured
}

// IsConfigured reports whether the provider has env keys present.
func (f *Factory) IsConfigured(name string) bool {
	_, ok := f.providers[name]
	return ok
}

// List returns ProviderInfo for every known provider. Used to render
// the integrations page (so the dashboard can show "Not configured" for
// providers with missing env keys, without crashing the page).
func (f *Factory) List() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(AllProviderNames))
	for _, name := range AllProviderNames {
		info := ProviderInfo{Name: name}
		if p, ok := f.providers[name]; ok {
			info.Configured = true
			info.DefaultScopes = p.DefaultScopes()
		} else {
			info.DefaultScopes = defaultScopesFor(name)
		}
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
