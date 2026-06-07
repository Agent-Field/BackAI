// SPDX-License-Identifier: Apache-2.0

// Package tenantctx is the read-side API for tenant + API-key identity
// stored on a request's context.Context.
//
// The Phase 6.1 tenant-resolver middleware populates the context with
// WithTenant() before downstream middleware (rate limit, audit) and
// handlers run. Until that resolver lands, callers receive zero-values
// from TenantID() / APIKeyID() — every dependent piece of code MUST
// tolerate empty strings gracefully (e.g. rate-limit uses a global
// bucket, audit writes NULL).
//
// Keeping the contract in its own tiny package means:
//   - The resolver lives in package internal/auth (or wherever Phase 6.1
//     decides) and only depends on this package.
//   - Middleware + handlers depend on this package, not on the resolver,
//     so the resolver can be swapped without touching consumers.
//   - Tests can synthesise context values without spinning up the
//     resolver.
package tenantctx

import "context"

// ctxKey is an unexported type to avoid collision with other context keys
// defined elsewhere in the binary (or third-party packages).
type ctxKey int

const (
	tenantIDKey ctxKey = iota
	apiKeyIDKey
	userIDKey
)

// WithTenant returns a copy of ctx that carries the given tenantID and
// apiKeyID. Either may be empty when not yet known (e.g. pre-auth hook
// fires with tenantID empty).
//
// The Phase 6.1 resolver calls this once per request after authenticating
// the bearer token / cookie and identifying the owning tenant.
func WithTenant(ctx context.Context, tenantID, apiKeyID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tenantID != "" {
		ctx = context.WithValue(ctx, tenantIDKey, tenantID)
	}
	if apiKeyID != "" {
		ctx = context.WithValue(ctx, apiKeyIDKey, apiKeyID)
	}
	return ctx
}

// TenantID returns the tenant id stored on ctx, or "" if absent.
// Callers MUST handle the empty case (no resolver wired, or
// unauthenticated path like /health).
func TenantID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(tenantIDKey).(string); ok {
		return v
	}
	return ""
}

// APIKeyID returns the api-key id stored on ctx, or "" if absent.
// Empty means the request used a session cookie (not an API key) or
// arrived before auth ran.
func APIKeyID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(apiKeyIDKey).(string); ok {
		return v
	}
	return ""
}

// WithTenantAndUser stores the tenant, api-key, and user id on the
// context in one call. The Phase 6.1 resolver uses this for the
// session-cookie path (where there's no api-key but there is a user
// id). Each value is optional — empty strings are not written so a
// later WithTenant(... "", "") doesn't clear an earlier value.
func WithTenantAndUser(ctx context.Context, tenantID, apiKeyID, userID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if tenantID != "" {
		ctx = context.WithValue(ctx, tenantIDKey, tenantID)
	}
	if apiKeyID != "" {
		ctx = context.WithValue(ctx, apiKeyIDKey, apiKeyID)
	}
	if userID != "" {
		ctx = context.WithValue(ctx, userIDKey, userID)
	}
	return ctx
}

// UserID returns the suite_users.id stored on ctx, or "" if absent.
// Set by the session-cookie path; the API-key path leaves it empty
// because keys aren't bound to a single user.
func UserID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}