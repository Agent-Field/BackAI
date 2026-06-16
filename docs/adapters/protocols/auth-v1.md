# Auth Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `auth` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/auth/Provider`

## Purpose

An auth adapter verifies session tokens, returns user and tenant
information, and optionally manages OAuth flows and multi-factor
authentication. The runtime consults the adapter during tenant resolution
to validate bearer tokens and populate the request context with user
identity and permissions.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/sessions/verify` | Verify token; return user, tenant, roles, expiry |
| `GET` | `/v1/users/{id}` | Return user metadata |
| `POST` | `/v1/oauth/{provider}/authorize` | Start OAuth flow |
| `POST` | `/v1/oauth/callback` | Complete OAuth flow |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

## 1. `POST /v1/sessions/verify`

Verify a bearer token and return the associated identity.

**Request body**:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

| Field | Required | Notes |
|---|---|---|
| `token` | yes | The session token to verify. |

**Response (200 OK)**:

```json
{
  "user_id": "usr_abc123",
  "email": "alice@example.com",
  "tenant_id": "ten_xyz789",
  "roles": ["admin", "editor"],
  "expires_at": "2026-06-15T12:00:00Z",
  "mfa_verified": true
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `user_id` | string | yes | Unique user identifier. |
| `email` | string | yes | User email address. |
| `tenant_id` | string | yes | The organization/workspace scope. |
| `roles` | array[string] | no | Authorization roles (empty list if none). |
| `expires_at` | string (ISO-8601) | yes | Token expiry timestamp (UTC). |
| `mfa_verified` | bool | no | Whether MFA has been completed for this session. Default false. |

**Errors**:

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_token` | 401 | Token format invalid or signature verification failed. |
| `expired_token` | 401 | Token valid but past expiry. |
| `unauthorized` | 401 | Bearer token (adapter auth) rejected. |
| `internal_error` | 500 | Catch-all. |

## 1a. `POST /v1/sessions/refresh`

Exchange a refresh token for a new access token. Clerk, Auth0,
Stytch — all use short-lived JWTs + refresh.

**Request body**:

```json
{"refresh_token": "rt_abc..."}
```

**Response (200 OK)**:

```json
{
  "token": "eyJ...",
  "refresh_token": "rt_xyz...",
  "expires_at": "2026-06-15T13:00:00Z",
  "user_id": "usr_abc123"
}
```

**Errors**: `invalid_token`, `expired_token` per §1.

Adapters that don't issue refresh tokens (better-auth's default
session model) MAY return `404 not_found` for this endpoint; the
runtime treats that as "refresh not supported, re-authenticate via
your normal flow."

## 1b. `POST /v1/sessions/revoke`

Sign-out / explicit session invalidation. Idempotent.

**Request body**:

```json
{"token": "eyJ..."}
```

**Response**: `204 No Content` on success. `401 invalid_token` if the
token isn't recognised. The adapter MUST mark the session unusable for
subsequent `/v1/sessions/verify` calls.

## 2. `GET /v1/users/{id}`

Return user metadata. Does NOT require the user to have an active session.

**Response (200 OK)**:

```json
{
  "id": "usr_abc123",
  "email": "alice@example.com",
  "name": "Alice Smith",
  "created_at": "2026-01-15T10:00:00Z",
  "mfa_enrolled": true,
  "providers": ["google", "github"]
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | string | yes | User ID (must match the path param). |
| `email` | string | yes | User email. |
| `name` | string | no | Display name. |
| `created_at` | string (ISO-8601) | no | Account creation timestamp (UTC). |
| `mfa_enrolled` | bool | no | Whether the user has MFA enabled. |
| `providers` | array[string] | no | OAuth providers connected to this account (e.g., ["google","github"]). |

**Errors**:

| Code | HTTP | Meaning |
|---|---|---|
| `user_not_found` | 404 | ID does not exist. |
| `unauthorized` | 401 | Bearer token (adapter auth) rejected. |
| `internal_error` | 500 | Catch-all. |

## 3. `POST /v1/oauth/{provider}/authorize`

Initiate an OAuth authorization flow. Returns the authorization URL the
client should redirect to.

**Path parameters**:

| Param | Notes |
|---|---|
| `provider` | OAuth provider name (e.g., `google`, `github`). |

**Request body**: empty `{}`.

**Response (200 OK)**:

```json
{
  "authorize_url": "https://accounts.google.com/o/oauth2/v2/auth?client_id=...",
  "state": "state_nonce_abc123"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `authorize_url` | string | yes | Full URL to send the user to. |
| `state` | string | yes | CSRF protection nonce; must be passed to `/callback`. |

**Errors**:

| Code | HTTP | Meaning |
|---|---|---|
| `provider_unavailable` | 422 | Provider is not configured or not in `supports_oauth_providers`. |
| `unauthorized` | 401 | Bearer token (adapter auth) rejected. |
| `internal_error` | 500 | Catch-all. |

## 4. `POST /v1/oauth/callback`

Complete an OAuth flow. The client exchanges the code and state returned
by the OAuth provider.

**Request body**:

```json
{
  "provider": "google",
  "code": "4/0AY0e-g...",
  "state": "state_nonce_abc123"
}
```

| Field | Required | Notes |
|---|---|---|
| `provider` | yes | OAuth provider name. |
| `code` | yes | Authorization code from the provider. |
| `state` | yes | State nonce from the previous `/authorize` call. |

**Response (200 OK)**:

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-06-15T12:00:00Z",
  "user_id": "usr_abc123"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `token` | string | yes | Session token (bearer format). |
| `expires_at` | string (ISO-8601) | yes | Token expiry (UTC). |
| `user_id` | string | yes | The user ID now logged in. |

**Errors**:

| Code | HTTP | Meaning |
|---|---|---|
| `oauth_state_mismatch` | 401 | State parameter doesn't match stored value; CSRF rejected. |
| `provider_unavailable` | 422 | Provider is not configured or not in `supports_oauth_providers`. |
| `unauthorized` | 401 | Bearer token (adapter auth) rejected. |
| `internal_error` | 500 | Catch-all. |

## 4a. Magic-link and MFA flows (out of scope for v1)

Adapters that support magic-link sign-in (Stytch, Supabase, Clerk) or
multi-factor auth handle those flows **internally** through their own
hosted UI. The runtime never invokes a "send magic link" API directly
— it only sees the resulting bearer token via `/v1/sessions/verify`.

For v1, the `supports_magic_links` and `supports_mfa` capabilities are
**advisory** — they tell the dashboard whether to surface UI hints
("this auth adapter supports MFA; enroll in your provider's
dashboard"). The runtime does not enforce or coordinate these flows.

A future protocol version will add `POST /v1/auth/magic-link/send`,
`POST /v1/auth/magic-link/verify`, and `POST /v1/auth/mfa/enroll` once
the runtime has a reason to drive them programmatically.

## 5. `GET /v1/capabilities`

```json
{
  "name": "better-auth",
  "version": "1.0.0",
  "slot": "auth",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "capabilities": {
    "supports_oauth_providers": ["google", "github", "microsoft"],
    "supports_magic_links": false,
    "supports_passwordless": true,
    "supports_mfa": true,
    "supports_sso": false,
    "session_lifetime_seconds": 3600,
    "supports_token_introspection": true
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `supports_oauth_providers` | array[string] | List of OAuth providers available (e.g., ["google", "github"]). Empty list if none. |
| `supports_magic_links` | bool | Whether magic-link authentication is available. |
| `supports_passwordless` | bool | Whether passwordless (e.g., passkey) authentication is available. |
| `supports_mfa` | bool | Whether multi-factor authentication is enforced or available. |
| `supports_sso` | bool | Whether SAML/SSO integration is available. |
| `session_lifetime_seconds` | int | How long sessions live before expiry. |
| `supports_token_introspection` | bool | Whether `/v1/sessions/verify` can introspect tokens (vs only verifying signatures). |

## 6. `GET /healthz` and `GET /v1/info`

Follow the common pattern from [`PROTOCOL.md`](../PROTOCOL.md).

## 7. Error codes reference

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_token` | 401 | Token malformed, invalid signature, or unknown algorithm. |
| `expired_token` | 401 | Token signature is valid but past expiry timestamp. |
| `user_not_found` | 404 | User ID does not exist. |
| `mfa_required` | 401 | MFA is required but not completed for this session. |
| `oauth_state_mismatch` | 401 | CSRF nonce mismatch in OAuth callback. |
| `provider_unavailable` | 422 | OAuth provider not configured or not supported. |
| `unauthorized` | 401 | Adapter's bearer token (from env) rejected by upstream. |
| `internal_error` | 500 | Unclassified server error. |

## 8. Behavior notes

- **No plaintext in logs.** Adapters MUST NOT log session tokens. Request/response bodies MUST be scrubbed of sensitive fields.
- **Token verification.** The adapter MUST verify signatures locally (when possible) to avoid latency on every verify call. If signature verification passes, the adapter MAY cache the decoded claims for brief periods (e.g., 10s) to reduce upstream queries for high-traffic verify calls.
- **Expiry validation.** The adapter MUST check `expires_at` against the current time and return `expired_token` if the token is past its deadline.
- **MFA state.** If MFA is required by policy and the session has not completed MFA challenges, the adapter SHOULD return `mfa_verified=false`. The runtime may prompt for MFA before granting access.
- **Tenant isolation.** The runtime enforces multi-tenancy boundaries; the adapter does not need to. However, adapters MAY record the `X-BackAI-Tenant-Id` header for audit purposes.

## 9. Mapping back to the Go interface

The auth.Provider interface methods map to HTTP calls:

| Go method | HTTP call |
|---|---|
| `VerifySession(ctx, token)` | `POST /v1/sessions/verify` |
| `GetUser(ctx, id)` | `GET /v1/users/{id}` |
| `Capabilities()` | cached result of `GET /v1/capabilities` |

OAuth and health/info endpoints are available via the `Client` but not exposed through the core `Provider` interface in v1 (future extension).

## 10. Conformance checklist

- [ ] `POST /v1/sessions/verify` with valid token returns `200` + Identity with user_id, email, tenant_id, expires_at
- [ ] `POST /v1/sessions/verify` with invalid token returns `401 + invalid_token`
- [ ] `POST /v1/sessions/verify` with expired token returns `401 + expired_token`
- [ ] `GET /v1/users/{id}` with valid user returns `200` + User metadata
- [ ] `GET /v1/users/{id}` with missing user returns `404 + user_not_found`
- [ ] `POST /v1/oauth/{provider}/authorize` returns `200` + authorize_url + state (if supports_oauth_providers is non-empty)
- [ ] `POST /v1/oauth/callback` with valid code and matching state returns `200` + token + expires_at + user_id
- [ ] `POST /v1/oauth/callback` with state mismatch returns `401 + oauth_state_mismatch`
- [ ] `GET /v1/capabilities` returns capability envelope with slot=auth and capabilities object
- [ ] Bearer auth enforced on all endpoints
- [ ] Errors return RFC 7807 problem details with `code` field
