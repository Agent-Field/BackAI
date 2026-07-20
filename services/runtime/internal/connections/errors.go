// SPDX-License-Identifier: Apache-2.0

package connections

import "errors"

// Sentinel errors. Callers (the server handler) branch on these via
// errors.Is to map to stable HTTP error codes; the codes themselves are
// assigned in the handler so the package stays transport-agnostic.
var (
	// ErrUnknownProvider is returned when a provider name matches no
	// registered Descriptor.
	ErrUnknownProvider = errors.New("connections: unknown provider")

	// ErrProviderNotConfigured is returned when an OAuth flow is requested
	// for a provider whose platform client id/secret are not configured on
	// this runtime.
	ErrProviderNotConfigured = errors.New("connections: provider not configured")

	// ErrKindUnsupported is returned when a requested kind isn't supported
	// for the provider (e.g. oauth kind for a provider with no token
	// endpoint).
	ErrKindUnsupported = errors.New("connections: kind unsupported for provider")

	// ErrNotFound is returned when no connection row matches (tenant, id).
	ErrNotFound = errors.New("connections: not found")

	// ErrRevoked is returned by Handle when the connection has been
	// revoked. Maps to 409 CONNECTION_REVOKED.
	ErrRevoked = errors.New("connections: revoked")

	// ErrScopeNotGranted is returned by Handle when the request targets a
	// provider operation whose required scope is not in the connection's
	// granted scopes.
	ErrScopeNotGranted = errors.New("connections: scope not granted")

	// ErrRefreshFailed wraps a token-refresh transport/parse failure. The
	// upstream provider body is stripped before wrapping so a provider
	// error page can't leak token material.
	ErrRefreshFailed = errors.New("connections: token refresh failed")

	// ErrCredentialRequired is returned when creating an api_key connection
	// without a credential.
	ErrCredentialRequired = errors.New("connections: credential required")

	// ErrValidation is returned for malformed create input.
	ErrValidation = errors.New("connections: validation failed")

	// ErrWebhookUnsupported is returned when verify-webhook is called for a
	// provider with no webhook signature scheme.
	ErrWebhookUnsupported = errors.New("connections: webhook verification unsupported for provider")
)
