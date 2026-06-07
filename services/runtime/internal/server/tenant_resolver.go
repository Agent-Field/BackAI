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
	"/health":       {},
	"/ready":        {},
	"/metrics":      {},
	"/openapi.json": {},
}

// publicPrefixes are path prefixes that bypass resolution. The
// dashboard's read-only endpoints (which run inside the operator's
// browser session, behind the dashboard's own auth) don't need a
// runtime-side tenant binding in v1.
var publicPrefixes = []string{
	"/api/v1/agents", // discovery is unauthenticated
	"/api/v1/runs",
	"/api/v1/home/overview",
	"/api/v1/cost",
	"/api/v1/modules",
	"/api/v1/queues",
	// The admin/* surface gates its own access via the
	// multi-tenancy module flag + admin auth (Phase 6 dashboard).
	"/api/v1/admin",
	// DB studio (Phase 8.1) — dashboard-only operator surface; the
	// dashboard's own session auth gates it, the same way admin
	// routes work.
	"/api/v1/db",
	// Memory (Phase 8.2). Operator can browse + put + search from the
	// dashboard. Tenant scoping is enforced at the Store layer via
	// the resolved tenant context when scope=tenant.
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
	// Billing dashboard surface (Phase 10.4). Same auth shape as
	// admin/* — the dashboard's session gates it.
	"/api/v1/billing",
}

func isPublicPath(p string) bool {
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
//                   only the default-tenant fallback works.
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

		// Public endpoints bypass entirely. They run with no tenant
		// context; handlers that touch tenant-scoped data MUST check
		// for an empty tenantctx.TenantID and refuse.
		if isPublicPath(path) {
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
				writeAuthError(w, http.StatusUnauthorized, "INVALID_API_KEY",
					"invalid API key")
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
	var token string
	for _, name := range []string{
		"better-auth.session_token",
		"__Secure-better-auth.session_token",
	} {
		if c, cookieErr := r.Cookie(name); cookieErr == nil && c.Value != "" {
			token = c.Value
			break
		}
	}
	if token == "" {
		return "", "", errNoSession
	}
	// better-auth signs cookies as "<token>.<base64-hmac>". The token row
	// in the `session` table only stores the bare token portion, so strip
	// any signature suffix before querying.
	if i := strings.IndexByte(token, '.'); i >= 0 {
		token = token[:i]
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
