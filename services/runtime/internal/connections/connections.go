// SPDX-License-Identifier: Apache-2.0

// Package connections implements R5: one secure connection contract for
// external services (GitHub, Stripe, Google, Slack, …).
//
// The design goals:
//
//   - App code NEVER sees raw credentials. It creates a connection once
//     (handing over an API key, or completing an OAuth consent round-trip),
//     then makes calls through the runtime HANDLE — POST
//     /api/v1/connections/{id}/request — passing {method, path, query,
//     headers, body}. The runtime injects the credential server-side,
//     refreshes expired OAuth tokens, and returns the provider's response.
//
//   - Credentials live encrypted at rest, reusing the exact AES-256-GCM
//     envelope that backs the secrets vault (internal/secrets/crypto.go).
//     A DB leak of suite_connections yields provider/scope/status/expiry
//     metadata but no usable secret.
//
//   - Providers are TYPED CAPABILITY DESCRIPTORS in an in-code registry.
//     Adding a provider = adding a Descriptor, never expanding trusted core
//     logic. Each descriptor declares its auth kind, OAuth token/refresh
//     endpoints, base API URL, auth-header shape, and webhook signature
//     scheme.
//
//   - Every lifecycle transition emits an audit event to
//     suite_connection_events (created / refreshed / revoked / health_check
//     / error).
//
// Tenant identity is never taken from the request body — it flows from the
// tenant resolver + Postgres RLS on suite_connections (FORCE ROW LEVEL
// SECURITY, per 00034_connections.sql).
package connections

import "time"

// Kind is a connection's credential kind.
const (
	KindOAuth  = "oauth"
	KindAPIKey = "api_key"
)

// Status is a connection's lifecycle state.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
	StatusError   = "error"
)

// Health is the derived reachability label surfaced on list responses.
const (
	HealthOK      = "ok"
	HealthExpired = "expired"
	HealthRevoked = "revoked"
	HealthError   = "error"
	HealthUnknown = "unknown"
)

// Connection is the public, credential-free view of a stored row. The raw
// API key / access token / refresh token NEVER appear here — the encrypted
// blob stays in the store and is only ever materialised server-side inside
// Service.Handle. JSON tags are the on-the-wire contract for the
// /api/v1/connections surface.
type Connection struct {
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	Kind             string     `json:"kind"`
	Name             string     `json:"name"`
	GrantedScopes    []string   `json:"granted_scopes"`
	RequestedScopes  []string   `json:"requested_scopes"`
	Status           string     `json:"status"`
	TokenExpiry      *time.Time `json:"token_expiry,omitempty"`
	HasWebhookSecret bool       `json:"has_webhook_secret"`
	CreatedBy        string     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	// Health is a derived field, not stored: it folds status + expiry into
	// a single at-a-glance label for the dashboard.
	Health string `json:"health,omitempty"`
}

// withHealth returns a copy of c with Health derived from status/expiry.
// now is injected so the label is deterministic in tests.
func (c Connection) withHealth(now time.Time) Connection {
	switch c.Status {
	case StatusRevoked:
		c.Health = HealthRevoked
	case StatusError:
		c.Health = HealthError
	case StatusActive:
		if c.TokenExpiry != nil && !c.TokenExpiry.After(now) {
			c.Health = HealthExpired
		} else {
			c.Health = HealthOK
		}
	default:
		c.Health = HealthUnknown
	}
	return c
}
