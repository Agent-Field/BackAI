// SPDX-License-Identifier: Apache-2.0

// Package oauth implements OAuth-on-behalf-of-user: the AF Stack runtime
// lets agents act as the user in third-party APIs. The first shipped
// adapters are Google and GitHub; the provider contract leaves room for
// more adapters without changing the persistence model.
//
// Design pillars:
//
//   - Tokens NEVER live in the suite_oauth_tokens row. The row stores
//     opaque references; the encrypted values live in the secrets vault
//     (suite_secrets) under keys like "oauth_<provider>_<user_id>" and
//     "oauth_<provider>_<user_id>_refresh". Defense in depth — a SQL
//     read of suite_oauth_tokens leaks scopes + expiry, never bytes the
//     attacker can authenticate with.
//   - Distinct from better-auth login OAuth. better-auth's GOOGLE_CLIENT_ID
//     handles operator/customer SIGN-IN. This package's
//     OAUTH_GOOGLE_CLIENT_ID handles AGENT acting AS the user in 3rd party
//     APIs — different scopes, different tokens, different storage.
//   - CSRF protection via signed state nonce (HMAC over tenant_id +
//     user_id + return_to + timestamp using AF_STACK_AUTH_SECRET).
//   - return_to validation against an allowed-origin list (customer-app).
//   - Refresh-on-expiry: token retrieval transparently refreshes inside
//     a 60s expiry window; failures bubble back with a typed error.
//
// The Provider interface is the surface every adapter implements. Each
// adapter is wired into the Factory based on env-driven config; an
// adapter is "configured" iff both its OAUTH_<NAME>_CLIENT_ID and
// OAUTH_<NAME>_CLIENT_SECRET env vars are set.
package oauth

import (
	"context"
	"errors"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// TokenSet is the canonical OAuth result, re-exported from oauthtypes
// so adapter code can import a sibling package without an import cycle
// back to oauth.
type TokenSet = oauthtypes.TokenSet

// Provider is the contract each third-party OAuth adapter implements.
// All methods are stateless from the Provider's perspective; persistence
// (token storage, refresh on expiry) is owned by Manager.
type Provider interface {
	// Name is the canonical lowercase identifier ("google", "github").
	// Used as the URL segment and the suite_oauth_tokens.provider column.
	Name() string

	// AuthorizeURL builds the provider's consent URL. The opaque state
	// is forwarded back via the callback and must round-trip unmodified.
	AuthorizeURL(state string, scopes []string, redirectURI string) string

	// Exchange swaps an authorization code for a TokenSet. Adapters
	// MUST NOT log the returned tokens.
	Exchange(ctx context.Context, code, redirectURI string) (TokenSet, error)

	// Refresh trades a refresh token for a new TokenSet. Returns
	// ErrRefreshNotSupported when the provider's flow has no refresh
	// path (GitHub user tokens, Notion, etc.).
	Refresh(ctx context.Context, refreshToken string) (TokenSet, error)

	// Revoke best-effort tells the provider to invalidate the token.
	// Adapters should return nil when the provider has no revoke
	// endpoint (caller still soft-deletes the row).
	Revoke(ctx context.Context, token string) error

	// DefaultScopes is the scope set requested when the caller doesn't
	// override scopes on /authorize.
	DefaultScopes() []string
}

// Connection is the public view of a stored token row. The plaintext
// access token NEVER appears on a Connection — only metadata. Use
// Manager.Token to retrieve the live access token via the vault.
type Connection struct {
	Provider    string     `json:"provider"`
	Scopes      []string   `json:"scopes"`
	ConnectedAt time.Time  `json:"connected_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// Sentinel errors. Callers branch on these via errors.Is.
var (
	// ErrProviderNotConfigured is returned by Factory.Get when the
	// requested provider has no OAUTH_<NAME>_CLIENT_ID / CLIENT_SECRET
	// pair in the environment.
	ErrProviderNotConfigured = errors.New("oauth: provider not configured")

	// ErrProviderUnknown is returned when the provider name doesn't
	// match any registered adapter.
	ErrProviderUnknown = errors.New("oauth: unknown provider")

	// ErrRefreshNotSupported is returned by Provider.Refresh when the
	// flow doesn't support refresh (GitHub user tokens, Notion). Same
	// value as oauthtypes.ErrRefreshNotSupported so errors.Is matches
	// across package boundaries.
	ErrRefreshNotSupported = oauthtypes.ErrRefreshNotSupported

	// ErrConnectionNotFound is returned by Manager.Token /
	// Manager.Disconnect when no row matches (tenant_id, user_id,
	// provider).
	ErrConnectionNotFound = errors.New("oauth: connection not found")

	// ErrInvalidState is returned when the callback state fails HMAC
	// verification (CSRF) or has expired.
	ErrInvalidState = errors.New("oauth: invalid state")

	// ErrInvalidReturnTo is returned when return_to isn't on the
	// allowed-origin list.
	ErrInvalidReturnTo = errors.New("oauth: return_to not allowed")

	// ErrUserRequired is returned when the caller is unauthenticated
	// (no user_id resolvable from the request context).
	ErrUserRequired = errors.New("oauth: user authentication required")

	// ErrTenantRequired is returned when tenant_id can't be resolved.
	ErrTenantRequired = errors.New("oauth: tenant context required")

	// ErrExchangeFailed wraps a provider Exchange/Refresh transport or
	// parse failure. Wrapped errors have the upstream HTTP body
	// stripped (provider error pages may include tokens). Same value
	// as oauthtypes.ErrExchangeFailed for cross-package errors.Is.
	ErrExchangeFailed = oauthtypes.ErrExchangeFailed
)
