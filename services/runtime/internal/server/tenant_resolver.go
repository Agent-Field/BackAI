// SPDX-License-Identifier: Apache-2.0

// tenant_resolver.go — per-request tenant identification middleware.
//
// Resolution order (first match wins):
//
//  1. Authorization: Bearer af_<prefix>_<secret>
//     -> tenancy.Manager.VerifyKey loads the key row, bcrypt-checks the
//        secret, and we attach (tenant_id, api_key_id) to the context.
//
//  2. Session cookie (better-auth)
//     -> we resolve session.userId -> suite_users.id -> first
//        suite_memberships row -> tenant_id. user_id is also attached.
//
//  3. Default tenant fallback (only when multi-tenancy is OFF)
//     -> the seeded "00000000-..." default tenant_id.
//
// When multi-tenancy is ON but the request carries no recognised
// credential we return 401 (Bearer) or 403 (session-without-membership)
// with the standard error envelope. The /health, /ready, /metrics,
// /openapi.json, and dashboard read-only endpoints are not gated by
// this middleware — see isPublicPath().
//
// Hook firing:
//   - HookGatewayPreAuth fires BEFORE any resolution attempt with a
//     payload {request_id, method, path, header_keys}. Handlers can
//     short-circuit by returning a non-nil error -> 401.
//   - HookGatewayPostAuth fires AFTER successful resolution with a
//     payload {request_id, tenant_id, api_key_id, user_id, source}.
//     Handlers MAY mutate the payload but most observe.
//
// PG session binding:
//   - On the response goroutine we DON'T bind a connection here —
//     the resolver runs before any DB-touching handler. Handlers
//     instead acquire a conn from the tenancy pool and call
//     tenancy.SetTenantContext(ctx, conn, tenantID) within their own
//     transaction. The middleware just makes the tenant id available
//     via the request context.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// resolveSource is the string we record on HookGatewayPostAuth so
// observers can attribute traffic by credential type.
type resolveSource string

const (
	srcAPIKey  resolveSource = "api_key"
	srcSession resolveSource = "session"
	srcDefault resolveSource = "default"
)

// publicPaths are the routes that bypass tenant resolution entirely.
// They serve cross-tenant diagnostics or the OpenAPI spec; gating them
// behind auth would break the operator's first boot.
var publicPaths = map[string]struct{}{
	"/health":              {},
	"/ready":               {},
	"/metrics":             {},
	"/openapi.json":        {},
	"/api/v1/openapi.json": {},
	"/api/v1/agents":       {},
}

// publicPrefixes are path prefixes that bypass resolution. The
// dashboard's read-only endpoints (which run inside the operator's
// browser session, behind the dashboard's own auth) don't need a
// runtime-side tenant binding in v1.
var publicPrefixes = []string{
	"/api/v1/runs",
	"/api/v1/home/overview",
	"/api/v1/cost",
	"/api/v1/modules",
	"/api/v1/queues",
	// The admin/* surface bypasses tenant resolution but each handler
	// enforces its own operator auth (adminAccessDenied for the tenancy
	// routes; operatorGuard for /admin/adapters).
	"/api/v1/admin",
	// DB studio (Phase 8.1) — operator-only. Bypasses tenant resolution,
	// but every /api/v1/db/* route is operator-gated in-handler via
	// operatorGuard (it can run arbitrary SQL), so a bare API key or an
	// unauthenticated caller gets 401.
	"/api/v1/db",
	// Memory (Phase 8.2). Only the operator LIST (GET /api/v1/memory)
	// is public here; the scoped get/put/delete/search ops are carved
	// out in tenantResolver so they bind a tenant (FORCE RLS on
	// suite_memory is the isolation boundary).
	"/api/v1/memory",
	// Sandboxes (Phase 9.1). Dashboard-driven operator surface; the
	// resolved tenant is read from the request when MT is on but the
	// endpoint set itself doesn't require API-key auth.
	"/api/v1/sandbox",
	// Notifications outbox view (Phase 10.1). Send is gated by tenant
	// auth like other writes; list/stats fall under dashboard auth.
	"/api/v1/notifications",
	// Webhooks (Phase 10.2 + 10.3) — endpoint configuration and delivery
	// log. /webhooks/in/* (inbound public endpoints) lives at the
	// gateway level and isn't covered by this prefix.
	"/api/v1/webhooks",
	// Phase 10.2 inbound surface. Providers POST raw payloads here;
	// the tenant resolver bypasses because the InboundService resolves
	// the endpoint's tenant from the slug-keyed row in PG.
	"/webhooks",
	// OAuth provider callbacks validate their own signed state because
	// providers may not return the same app session cookie shape.
	"/oauth/callback",
	// Billing dashboard surface (Phase 10.4). Same auth shape as
	// admin/* — the dashboard's session gates it.
	"/api/v1/billing",
	// MCP servers + tools (Phase 11.1). Dashboard CRUD surface; tool
	// calls inherit the caller's tenant context.
	"/api/v1/mcp",
	// Skills (Phase 11.3) — install/list/attach surface.
	"/api/v1/skills",
	// Secrets + config are operator dashboard surfaces. The handlers use
	// default-tenant semantics until full per-tenant dashboard switching lands.
	"/api/v1/secrets",
	"/api/v1/config",
	// Harnesses (Phase 11.4) — probe + list surface for the dashboard.
	"/api/v1/harnesses",
	// Crons (Phase 12.2) — scheduled-job CRUD surface.
	"/api/v1/crons",
	// Metrics summary (Phase 12.2) — at-a-glance Prometheus rollup.
	"/api/v1/metrics",
	// Plugins manifest (Phase 12.3) — dashboard reads to render nav.
	"/api/v1/plugins",
	// Logs view (Phase 12.2) — dashboard reads from the runtime log ring.
	"/api/v1/logs",
}

func isPublicPath(p string) bool {
	// Tenant-scoped webhook routes must go THROUGH the resolver even though
	// the rest of /api/v1/webhooks is operator-gated + public-prefixed. A
	// tenant registers its own subscribers and emits its own events; RLS +
	// the emit fan-out scope both to the bound tenant. Without this the
	// prefix match below would bypass resolution and leave app.tenant_id
	// unbound, breaking the RLS-scoped subscription writes.
	if p == "/api/v1/webhooks/emit" ||
		p == "/api/v1/webhooks/subscriptions" ||
		strings.HasPrefix(p, "/api/v1/webhooks/subscriptions/") {
		return false
	}
	if _, ok := publicPaths[p]; ok {
		return true
	}
	for _, pref := range publicPrefixes {
		if p == pref || strings.HasPrefix(p, pref+"/") {
			return true
		}
	}
	return false
}

// tenantResolver returns a middleware that runs the resolution chain
// described in the file header. It depends on:
//
//   - s.tenancy   — required for API-key and session paths; when nil
//     only the default-tenant fallback works.
//   - s.db        — required for session lookups (better-auth tables).
//   - s.hooks     — optional; pre/post-auth hooks fire when non-nil.
//
// When multi-tenancy is OFF (s.multiTenancyEnabled() == false), every
// request gets the default tenant and no auth checks run — this keeps
// the runtime usable for single-tenant deployments.
func (s *Server) tenantResolver(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		path := r.URL.Path

		// Memory scoped ops (get/put/delete/search) must bind a tenant.
		// They used to ride the /api/v1/memory public prefix and trust a
		// client-supplied scope_id, which let any caller reach another
		// tenant's entries. Force them through auth so tenant_id (FORCE
		// RLS on suite_memory) is bound from a real credential. Only the
		// operator LIST (GET /api/v1/memory) stays public — it is
		// operatorGuard-gated and bypasses RLS to browse cross-tenant.
		scopedMemory := path == "/api/v1/memory/get" ||
			path == "/api/v1/memory/search" ||
			(path == "/api/v1/memory" && (r.Method == http.MethodPut || r.Method == http.MethodDelete))

		// Public endpoints bypass entirely. They run with no tenant
		// context; handlers that touch tenant-scoped data MUST check
		// for an empty tenantctx.TenantID and refuse.
		if !scopedMemory && isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		// Pre-auth hook: handlers see the bare request and can decide
		// to short-circuit (e.g. IP allowlist). Mutating the payload
		// is allowed but observed here only.
		preAuthPayload := map[string]any{
			"method": r.Method,
			"path":   path,
		}
		if s.hooks != nil {
			if _, err := s.hooks.Fire(ctx, hooks.HookGatewayPreAuth, preAuthPayload); err != nil {
				s.log.Warn("gateway pre-auth hook denied request",
					"path", path, "error", err)
				writeAuthError(w, http.StatusUnauthorized, "PRE_AUTH_DENIED",
					"pre-auth check failed")
				return
			}
		}

		// Multi-tenancy off: default tenant for everything.
		if !s.multiTenancyEnabled() {
			ctx = tenantctx.WithTenantAndUser(ctx, tenancy.DefaultTenantID, "", "")
			s.firePostAuth(ctx, preAuthPayload, tenancy.DefaultTenantID, "", "", srcDefault)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Try API key first. The Authorization header takes
		// precedence over the session cookie when both are present.
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			tenantID, apiKeyID, err := s.resolveBearer(ctx, token)
			if err != nil {
				writeBearerError(w, err)
				return
			}
			ctx = tenantctx.WithTenantAndUser(ctx, tenantID, apiKeyID, "")
			s.firePostAuth(ctx, preAuthPayload, tenantID, apiKeyID, "", srcAPIKey)
			// Best-effort touch — we don't block on the write.
			go func(id string) {
				touchCtx, cancel := context.WithTimeout(context.Background(), 2_000_000_000)
				defer cancel()
				s.tenancy.TouchKey(touchCtx, id)
			}(apiKeyID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// WebSocket clients in browsers cannot attach arbitrary headers.
		// Accept api_key on the realtime handshakes only; normal HTTP
		// surfaces must continue using Authorization: Bearer. Both the
		// table stream (/api/v1/realtime) and the run stream
		// (/api/v1/realtime/runs) are browser-reachable WS endpoints and
		// both SDKs authenticate them via ?api_key=.
		if path == "/api/v1/realtime" || path == "/api/v1/realtime/runs" {
			if token := strings.TrimSpace(r.URL.Query().Get("api_key")); token != "" {
				tenantID, apiKeyID, err := s.resolveBearer(ctx, token)
				if err != nil {
					writeBearerError(w, err)
					return
				}
				ctx = tenantctx.WithTenantAndUser(ctx, tenantID, apiKeyID, "")
				s.firePostAuth(ctx, preAuthPayload, tenantID, apiKeyID, "", srcAPIKey)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// Fall through to session resolution. We only run this when
		// the runtime has a DB connection AND a tenancy manager —
		// without either we can't translate session -> tenant.
		if s.db != nil && s.tenancy != nil {
			tenantID, userID, err := s.resolveSession(ctx, r)
			switch {
			case err == nil:
				ctx = tenantctx.WithTenantAndUser(ctx, tenantID, "", userID)
				s.firePostAuth(ctx, preAuthPayload, tenantID, "", userID, srcSession)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			case errors.Is(err, errNoSession):
				// No session cookie at all — fall through to the
				// 401 below.
			case errors.Is(err, tenancy.ErrMembershipNotFound):
				writeAuthError(w, http.StatusForbidden, "NO_TENANT_MEMBERSHIP",
					"session has no tenant membership")
				return
			default:
				s.log.Warn("session resolution failed",
					"path", path, "error", err)
			}
		}

		// No credentials, MT on: deny.
		writeAuthError(w, http.StatusUnauthorized, "UNAUTHENTICATED",
			"missing or invalid credentials")
	})
}

// firePostAuth invokes HookGatewayPostAuth with the resolved
// principal. Failures are logged but don't abort the request — a
// post-auth hook is by definition advisory.
func (s *Server) firePostAuth(
	ctx context.Context,
	pre map[string]any,
	tenantID, apiKeyID, userID string,
	source resolveSource,
) {
	if s.hooks == nil {
		return
	}
	payload := map[string]any{
		"method":     pre["method"],
		"path":       pre["path"],
		"tenant_id":  tenantID,
		"api_key_id": apiKeyID,
		"user_id":    userID,
		"source":     string(source),
	}
	if _, err := s.hooks.Fire(ctx, hooks.HookGatewayPostAuth, payload); err != nil {
		s.log.Warn("gateway post-auth hook returned error",
			"path", pre["path"], "error", err)
	}
}

// resolveBearer validates `token` against suite_api_keys via the
// tenancy manager. Returns the tenant id and api-key id on success.
// All error modes collapse to a single sentinel so callers can't tell
// invalid format from revoked key from missing key.
func (s *Server) resolveBearer(ctx context.Context, token string) (tenantID, apiKeyID string, err error) {
	if s.tenancy == nil {
		return "", "", errors.New("tenancy manager not configured")
	}
	k, vErr := s.tenancy.VerifyKey(ctx, token)
	if vErr != nil {
		// Differentiate in logs only — never in the 401 envelope.
		switch {
		case errors.Is(vErr, tenancy.ErrInvalidAPIKeyFormat):
			s.log.Debug("bearer: invalid format")
		case errors.Is(vErr, tenancy.ErrAPIKeyNotFound):
			s.log.Debug("bearer: no such key")
		case errors.Is(vErr, tenancy.ErrKeySecretMismatch):
			s.log.Debug("bearer: secret mismatch")
		case errors.Is(vErr, tenancy.ErrKeyRevoked):
			s.log.Info("bearer: revoked key attempted", "key_id", k.ID)
		case errors.Is(vErr, tenancy.ErrKeyExpired):
			s.log.Info("bearer: expired key attempted", "key_id", k.ID)
		default:
			s.log.Warn("bearer: verify failed", "error", vErr)
		}
		return "", "", vErr
	}
	return k.TenantID, k.ID, nil
}

// writeBearerError maps a resolveBearer failure to the response envelope.
// Expired keys get a distinct 401 KEY_EXPIRED so a caller who legitimately
// holds the key (the secret matched — expiry is only checked AFTER bcrypt
// succeeds, so this never leaks whether an arbitrary key exists) can tell an
// expired credential from a wrong one and rotate it. Every other failure
// mode collapses to INVALID_API_KEY to avoid an existence oracle.
func writeBearerError(w http.ResponseWriter, err error) {
	if errors.Is(err, tenancy.ErrKeyExpired) {
		writeAuthError(w, http.StatusUnauthorized, "KEY_EXPIRED",
			"API key has expired")
		return
	}
	writeAuthError(w, http.StatusUnauthorized, "INVALID_API_KEY",
		"invalid API key")
}

// errNoSession is returned by resolveSession when the request has no
// session cookie. Callers treat this as fall-through to 401, not as a
// hard error.
var errNoSession = errors.New("server: no session")

// resolveSession reads the better-auth session cookie, looks up the
// session in the "session" table, joins to "user", then finds the
// user's first membership in suite_memberships. Returns the membership
// tenant id and the canonical suite_users.id.
//
// better-auth defaults the cookie name to `better-auth.session_token`
// (or `__Secure-better-auth.session_token` over TLS). We accept either.
func (s *Server) resolveSession(ctx context.Context, r *http.Request) (tenantID, userID string, err error) {
	token := betterAuthSessionToken(r)
	if token == "" {
		return "", "", errNoSession
	}

	// Resolve session -> better-auth user id -> suite_users.id via
	// email join (the dashboard mirrors better-auth users into
	// suite_users at signup, keyed by email).
	var (
		baUserID string
		email    string
	)
	err = s.db.Pool.QueryRow(ctx, `
		select u."id", u."email"
		from "session" s
		join "user" u on u."id" = s."userId"
		where s."token" = $1 and s."expiresAt" > now()
		limit 1
	`, token).Scan(&baUserID, &email)
	if err != nil {
		// pgx returns ErrNoRows; we treat any "no row" as no session
		// (don't leak whether the token format was wrong).
		return "", "", errNoSession
	}

	if err := s.db.Pool.QueryRow(ctx,
		`select id::text from suite_users where email = $1 and deleted_at is null`,
		email).Scan(&userID); err != nil {
		return "", "", errNoSession
	}

	mem, err := s.tenancy.FirstMembershipFor(ctx, userID)
	if err != nil {
		return "", "", err
	}
	return mem.TenantID, userID, nil
}

func betterAuthSessionToken(r *http.Request) string {
	var token string
	// The operator dashboard uses a distinct cookie prefix
	// ("backai-operator") so its session can't collide with the customer
	// app's on a shared host — browsers scope cookies by host, not port.
	// Check the operator cookie first: it is the only one that can pass
	// the suite_operators gate, and each app's /api/v1 proxy forwards
	// only its own session cookie, so requests stay single-session.
	for _, name := range []string{
		"backai-operator.session_token",
		"__Secure-backai-operator.session_token",
		"better-auth.session_token",
		"__Secure-better-auth.session_token",
	} {
		if c, cookieErr := r.Cookie(name); cookieErr == nil && c.Value != "" {
			token = c.Value
			break
		}
	}
	// better-auth signs cookies as "<token>.<base64-hmac>". The token row
	// in the `session` table only stores the bare token portion, so strip
	// any signature suffix before querying.
	if i := strings.IndexByte(token, '.'); i >= 0 {
		token = token[:i]
	}
	return token
}

// writeAuthError emits the standard error envelope with a fixed code.
// Kept here (not in dashboard.go's helpers) because the resolver runs
// before any per-handler logging context exists.
func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

// Compile-time guard against the slog import being removed by a
// future refactor. The resolver uses slog through s.log.
var _ = slog.Default
