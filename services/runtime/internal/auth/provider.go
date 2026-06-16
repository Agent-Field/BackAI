// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"time"
)

// Identity represents a verified session and the user/tenant it belongs to.
type Identity struct {
	UserID      string
	Email       string
	TenantID    string
	Roles       []string
	ExpiresAt   time.Time
	MFAVerified bool
}

// User represents metadata for a user account.
type User struct {
	ID          string
	Email       string
	Name        string
	CreatedAt   time.Time
	MFAEnrolled bool
	Providers   []string // OAuth providers linked to account (e.g., ["google", "github"])
}

// Capabilities declares what this auth adapter supports.
type Capabilities struct {
	SupportsOAuthProviders  []string `json:"supports_oauth_providers"`
	SupportsMagicLinks      bool     `json:"supports_magic_links"`
	SupportsPasswordless    bool     `json:"supports_passwordless"`
	SupportsMFA             bool     `json:"supports_mfa"`
	SupportsSSO             bool     `json:"supports_sso"`
	SessionLifetimeSeconds  int      `json:"session_lifetime_seconds"`
	SupportsTokenIntrospect bool     `json:"supports_token_introspection"`
}

// RefreshedSession is the result of exchanging a refresh token. The
// new access token + (rotated) refresh token + expiry. Refresh-token
// rotation is provider-driven; the runtime doesn't enforce a policy.
type RefreshedSession struct {
	Token        string
	RefreshToken string
	ExpiresAt    time.Time
	UserID       string
}

// Provider is the abstract auth backend. The remote-adapter shim in
// adapters/remote/ satisfies it via the HTTP protocol from
// docs/adapters/protocols/auth-v1.md. Other callers should depend on
// this interface rather than on specific implementations so swapping
// the backend (better-auth, Auth0, Clerk, etc.) doesn't require code
// changes elsewhere.
//
// All methods take a context and respect cancellation. Implementations
// must be safe for concurrent use from multiple goroutines.
type Provider interface {
	// VerifySession verifies a bearer token and returns the decoded
	// Identity. Returns ErrInvalidToken if the token is malformed or
	// signature verification fails. Returns ErrExpiredToken if the token
	// is structurally valid but past its expiry time.
	VerifySession(ctx context.Context, token string) (Identity, error)

	// RefreshSession exchanges a refresh token for a new access token.
	// Returns ErrRefreshNotSupported when the adapter doesn't issue
	// refresh tokens (better-auth's default session model returns this
	// — the runtime then prompts the user to re-authenticate via the
	// normal flow).
	RefreshSession(ctx context.Context, refreshToken string) (RefreshedSession, error)

	// RevokeSession invalidates a session. Idempotent — calling on an
	// already-invalid token returns nil. Calling on an unknown token
	// returns ErrInvalidToken.
	RevokeSession(ctx context.Context, token string) error

	// GetUser returns metadata for a user by ID. Returns ErrUserNotFound
	// if the user does not exist. Does not require the user to have an
	// active session.
	GetUser(ctx context.Context, id string) (User, error)

	// Capabilities returns the capabilities and feature flags supported
	// by this adapter.
	Capabilities() Capabilities
}

// Sentinel errors returned by Provider methods. Adapters should map
// their backend-specific errors to these.
var (
	ErrInvalidToken       = errors.New("auth: invalid token")
	ErrExpiredToken       = errors.New("auth: token expired")
	ErrUserNotFound       = errors.New("auth: user not found")
	ErrRefreshNotSupported = errors.New("auth: refresh not supported by adapter")
)

// Compile-time assertion stub. Replace with actual adapter type.
var _ Provider = (*stubProvider)(nil)

// stubProvider is a compile-time check placeholder.
type stubProvider struct{}

func (*stubProvider) VerifySession(ctx context.Context, token string) (Identity, error) {
	panic("stub")
}

func (*stubProvider) RefreshSession(ctx context.Context, refreshToken string) (RefreshedSession, error) {
	panic("stub")
}

func (*stubProvider) RevokeSession(ctx context.Context, token string) error {
	panic("stub")
}

func (*stubProvider) GetUser(ctx context.Context, id string) (User, error) {
	panic("stub")
}

func (*stubProvider) Capabilities() Capabilities {
	panic("stub")
}
