// SPDX-License-Identifier: Apache-2.0

// scopes.go — per-route API-key scope enforcement (PRD R1).
//
// Background
//
// suite_api_keys carry a scopes text[] (migration 00001). Until now the
// tenant resolver authenticated a key and bound its tenant, but never
// consulted the scopes — only the operator gate (admin_rbac.go) read the
// operator-plane scopes. This file is the central route -> required-scope
// declaration + the enforcement primitive that closes that gap for the
// tenant API surface.
//
// Model
//
//   - Scope taxonomy is "<area>:read" / "<area>:write" plus the coarse
//     grants "admin" and "*".
//   - A key with NO scopes (empty/NULL) or a "*"/"admin" grant has full
//     tenant access — the legacy default. This is what keeps the
//     60-second quickstart (and every key minted before scopes were
//     enforced) working unchanged.
//   - A key with explicit narrow scopes is enforced strictly, fail-closed:
//     a route it lacks the scope for returns 403 with the structured code
//     SCOPE_DENIED naming the missing scope.
//   - Session (browser) principals are NOT scope-gated — operator/tenant
//     roles govern them. The default-tenant principal (multi-tenancy off)
//     is likewise unrestricted.
//
// The same requiredScopeFor registry is the single source of truth for
// both runtime enforcement (tenant_resolver.go) and the OpenAPI
// x-required-scope / x-principals annotations (see AnnotateSecurityFunc).

package server

import (
	"net/http"
	"strings"
)

// isFullAccessScopes reports whether a held scope set grants unrestricted
// tenant access: an empty/nil set (legacy keys predate scopes) or any of
// the coarse "*"/"admin" grants.
func isFullAccessScopes(held []string) bool {
	if len(held) == 0 {
		return true
	}
	for _, h := range held {
		switch strings.TrimSpace(h) {
		case "*", "admin":
			return true
		}
	}
	return false
}

// splitScope splits "<area>:<action>" into its parts. A scope with no
// colon (e.g. a bare area) returns an empty action.
func splitScope(s string) (area, action string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// scopeSatisfied reports whether a key holding `held` may call a route
// requiring `required`. An empty requirement is always satisfied.
//
// A narrow key satisfies a requirement when it holds:
//   - the exact scope ("storage:write" for "storage:write"), or
//   - a bare-area or area-wildcard grant ("storage" / "storage:*"), or
//   - the area's write scope for a read requirement (write implies read).
//
// Full-access keys (empty/nil, "*", "admin") satisfy everything.
func scopeSatisfied(held []string, required string) bool {
	if required == "" || isFullAccessScopes(held) {
		return true
	}
	reqArea, reqAction := splitScope(required)
	for _, h := range held {
		if strings.TrimSpace(h) == required {
			return true
		}
		area, action := splitScope(h)
		if area != reqArea {
			continue
		}
		switch action {
		case "", "*":
			// Bare area ("storage") or area wildcard ("storage:*") grants
			// every action on the area.
			return true
		case "write":
			if reqAction == "read" {
				return true
			}
		}
	}
	return false
}

// rwScope maps an HTTP method to the read/write scope for an area:
// read-only verbs (GET/HEAD/OPTIONS) require "<area>:read", everything
// else requires "<area>:write".
func rwScope(area, method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return area + ":read"
	default:
		return area + ":write"
	}
}

// requiredScopeFor returns the scope a tenant API key must hold to call
// (method, path), or "" when the route imposes no tenant-scope
// requirement. It matches on the request path (real or OpenAPI-templated
// — the prefixes work for both), so it is the single source of truth for
// enforcement AND for the OpenAPI security annotations.
//
// Only the families a tenant API key actually operates on are mapped.
// Some listed families (secrets, admin) additionally sit behind the
// operator gate + the tenant-resolver public-prefix bypass; their entries
// here document the scope in OpenAPI even though a tenant key is already
// refused by the operator gate before scope enforcement would run.
func requiredScopeFor(method, path string) string {
	switch {
	// Storage — read for GET (list/signed-url/download), write for
	// upload/delete.
	case strings.HasPrefix(path, "/api/v1/storage"):
		return rwScope("storage", method)

	// LLM gateway. Cache admin is operator-only (no tenant scope); model
	// discovery is a read; every generation endpoint is a write.
	case strings.HasPrefix(path, "/api/v1/llm/cache"):
		return ""
	case path == "/api/v1/llm/models":
		return "llm:read"
	case strings.HasPrefix(path, "/api/v1/llm"),
		strings.HasPrefix(path, "/api/v1/embeddings"),
		strings.HasPrefix(path, "/api/v1/images"),
		strings.HasPrefix(path, "/api/v1/audio"):
		return "llm:write"

	// Agent invocation + execution control.
	case strings.HasPrefix(path, "/api/v1/agents/"):
		return "agents:write"
	case strings.HasPrefix(path, "/api/v1/executions"):
		return rwScope("agents", method)

	// Background jobs.
	case strings.HasPrefix(path, "/api/v1/jobs"):
		return rwScope("jobs", method)

	// App-data search. The bare POST /search is a query (read); the
	// index/document mutations are writes.
	case path == "/api/v1/search":
		return "search:read"
	case strings.HasPrefix(path, "/api/v1/search"):
		return rwScope("search", method)

	// User/product activity log.
	case strings.HasPrefix(path, "/api/v1/activity"):
		return rwScope("activity", method)

	// Suite memory. get/search are reads regardless of HTTP verb;
	// put/delete are writes.
	case path == "/api/v1/memory/get" || path == "/api/v1/memory/search":
		return "memory:read"
	case strings.HasPrefix(path, "/api/v1/memory"):
		return rwScope("memory", method)

	// Tenant webhook subscribe/emit (the tenant-scoped routes carved out
	// of the operator webhook surface).
	case path == "/api/v1/webhooks/emit":
		return "webhooks:write"
	case strings.HasPrefix(path, "/api/v1/webhooks/subscriptions"):
		return rwScope("webhooks", method)

	// Secrets + admin — documented for OpenAPI; operator-gated at runtime.
	case strings.HasPrefix(path, "/api/v1/secrets"):
		return rwScope("secrets", method)
	case strings.HasPrefix(path, "/api/v1/admin"):
		return rwScope("admin", method)
	}
	return ""
}

// scopeDenied enforces the route's required scope against a resolved API
// key's held scopes. It returns true (and writes a 403 SCOPE_DENIED
// envelope naming the missing scope) when the key is too narrow; false
// when the request may proceed. Callers pass this only for API-key
// principals — session and default-tenant principals are never
// scope-gated.
func (s *Server) scopeDenied(w http.ResponseWriter, r *http.Request, held []string) bool {
	required := requiredScopeFor(r.Method, r.URL.Path)
	if scopeSatisfied(held, required) {
		return false
	}
	writeAuthError(w, http.StatusForbidden, "SCOPE_DENIED",
		"api key is missing the required scope: "+required)
	return true
}

// scopeAndPrincipalsFor is the OpenAPI annotation callback: for a
// registered (method, path) it returns the required scope and the
// principal types that may satisfy it. Empty scope means "not annotated".
func scopeAndPrincipalsFor(method, path string) (scope string, principals []string) {
	scope = requiredScopeFor(method, path)
	if scope == "" {
		return "", nil
	}
	if strings.HasPrefix(path, "/api/v1/admin") || strings.HasPrefix(path, "/api/v1/secrets") {
		// These surfaces are operator-gated: an operator browser session or
		// an operator-scoped API key, never a plain tenant key.
		return scope, []string{"operator_session", "operator_key"}
	}
	return scope, []string{"api_key", "session"}
}
