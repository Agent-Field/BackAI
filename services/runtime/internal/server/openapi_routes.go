// SPDX-License-Identifier: Apache-2.0

// openapi_routes.go — sub-handler OpenAPI registrations.
//
// We keep these calls OUT of the handler files (storage.go, secrets.go,
// admin.go) so each handler stays focused on request/response logic.
// All the spec metadata lives in one place so a reader can audit the
// public API surface in a single read.
//
// Schema components for known response shapes are seeded here too.
package server

import (
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// registerStorageOpenAPI describes /api/v1/storage/* routes.
func (s *Server) registerStorageOpenAPI() {
	b := s.openapi
	b.Register("POST", "/api/v1/storage/upload", openapi.RouteMeta{
		Summary: "Upload an object (multipart)", Tags: []string{"storage"},
	})
	b.Register("GET", "/api/v1/storage/signed-url", openapi.RouteMeta{
		Summary: "Generate a presigned GET URL", Tags: []string{"storage"},
		Parameters: []openapi.Parameter{
			{Name: "key", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "ttl", In: "query", Required: false,
				Description: "Seconds; default 3600, max 604800",
				Schema:      map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/storage", openapi.RouteMeta{
		Summary: "List objects under a prefix", Tags: []string{"storage"},
		Parameters: []openapi.Parameter{
			{Name: "prefix", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "next_token", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/storage/{key}", openapi.RouteMeta{
		Summary: "Download an object by key", Tags: []string{"storage"},
		Parameters: []openapi.Parameter{
			{Name: "key", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "width", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "height", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "transform", In: "query",
				Schema: map[string]any{"type": "string", "enum": []string{"resize", "thumbnail"}}},
			{Name: "format", In: "query",
				Schema: map[string]any{"type": "string", "enum": []string{"png", "jpeg", "gif"}}},
		},
	})
	b.Register("DELETE", "/api/v1/storage/{key}", openapi.RouteMeta{
		Summary: "Delete an object by key", Tags: []string{"storage"},
		Parameters: []openapi.Parameter{
			{Name: "key", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
}

// registerSecretsOpenAPI describes /api/v1/secrets/* routes.
func (s *Server) registerSecretsOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/secrets", openapi.RouteMeta{
		Summary: "List secrets (metadata only)", Tags: []string{"secrets"},
	})
	b.Register("GET", "/api/v1/secrets/{key}", openapi.RouteMeta{
		Summary: "Get secret metadata (no value)", Tags: []string{"secrets"},
	})
	b.Register("PUT", "/api/v1/secrets/{key}", openapi.RouteMeta{
		Summary: "Create or replace a secret", Tags: []string{"secrets"},
	})
	b.Register("DELETE", "/api/v1/secrets/{key}", openapi.RouteMeta{
		Summary: "Delete a secret", Tags: []string{"secrets"},
	})
	b.Register("POST", "/api/v1/secrets/{key}/reveal", openapi.RouteMeta{
		Summary: "Reveal the plaintext value (audited)", Tags: []string{"secrets"},
	})
	b.Register("POST", "/api/v1/secrets/{key}/rotate", openapi.RouteMeta{
		Summary: "Rotate the stored value", Tags: []string{"secrets"},
	})
}

// registerMemoryOpenAPI describes /api/v1/memory/* routes.
func (s *Server) registerMemoryOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/memory", openapi.RouteMeta{
		Summary: "List memory entries (paginated)", Tags: []string{"memory"},
		Parameters: []openapi.Parameter{
			{Name: "scope", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "scope_id", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "prefix", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/memory/get", openapi.RouteMeta{
		Summary: "Get a single memory entry", Tags: []string{"memory"},
		Parameters: []openapi.Parameter{
			{Name: "scope", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "key", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "scope_id", In: "query",
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("PUT", "/api/v1/memory", openapi.RouteMeta{
		Summary: "Upsert a memory entry", Tags: []string{"memory"},
	})
	b.Register("DELETE", "/api/v1/memory", openapi.RouteMeta{
		Summary: "Delete a memory entry", Tags: []string{"memory"},
		Parameters: []openapi.Parameter{
			{Name: "scope", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "key", In: "query", Required: true,
				Schema: map[string]any{"type": "string"}},
			{Name: "scope_id", In: "query",
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/memory/search", openapi.RouteMeta{
		Summary: "Vector-similarity search over memory", Tags: []string{"memory"},
	})
}

// registerSearchOpenAPI describes the app-data search routes.
func (s *Server) registerSearchOpenAPI() {
	b := s.openapi
	b.Register("POST", "/api/v1/search", openapi.RouteMeta{
		Summary: "Search tenant-scoped app data", Tags: []string{"search"},
	})
	b.Register("GET", "/api/v1/search/indexes", openapi.RouteMeta{
		Summary: "List search index statistics", Tags: []string{"search"},
	})
	b.Register("PUT", "/api/v1/search/documents", openapi.RouteMeta{
		Summary: "Upsert a searchable app-data document", Tags: []string{"search"},
	})
	b.Register("DELETE", "/api/v1/search/documents/{namespace}/{key}", openapi.RouteMeta{
		Summary: "Delete a searchable app-data document", Tags: []string{"search"},
		Parameters: []openapi.Parameter{
			{Name: "namespace", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
			{Name: "key", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}

// registerActivityOpenAPI describes the user activity log routes.
func (s *Server) registerActivityOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/activity", openapi.RouteMeta{
		Summary: "List tenant-scoped user activity", Tags: []string{"activity"},
		Parameters: []openapi.Parameter{
			{Name: "user_id", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "action", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "resource_type", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "resource_id", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "from", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
			{Name: "to", In: "query", Schema: map[string]any{"type": "string", "format": "date-time"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("POST", "/api/v1/activity", openapi.RouteMeta{
		Summary: "Append a user activity event", Tags: []string{"activity"},
	})
}

// registerConfigOpenAPI describes runtime config routes.
func (s *Server) registerConfigOpenAPI() {
	b := s.openapi
	b.Register("GET", "/api/v1/config/flags", openapi.RouteMeta{
		Summary: "List runtime feature flags", Tags: []string{"config"},
	})
	b.Register("PUT", "/api/v1/config/flags/{key}", openapi.RouteMeta{
		Summary: "Set a runtime feature flag", Tags: []string{"config"},
		Parameters: []openapi.Parameter{
			{Name: "key", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}

// NOTE: registerAdminOpenAPI lives in admin.go (alongside the handler
// declarations). It used to be here, but co-locating it with the
// handlers makes future param/response schema edits easier.

// registerRunAgentFieldOpenAPI describes the #25 per-run AgentField
// proxy routes:
//
//	GET  /api/v1/runs/{id}/agentfield        — summary + deep-link URL
//	POST /api/v1/runs/{id}/cancel
//	POST /api/v1/runs/{id}/pause
//	POST /api/v1/runs/{id}/resume
//	POST /api/v1/runs/{id}/request-approval
//
// Handlers live in internal/server/run_agentfield.go. We don't model the
// AgentField response schema here — it's exposed as a `extra` JSON blob
// so AgentField additive schema changes don't require a runtime release.
func (s *Server) registerRunAgentFieldOpenAPI() {
	b := s.openapi
	idParam := openapi.Parameter{
		Name: "id", In: "path", Required: true,
		Description: "AgentField run id",
		Schema:      map[string]any{"type": "string"},
	}
	b.Register("GET", "/api/v1/runs/{id}/agentfield", openapi.RouteMeta{
		Summary:    "AgentField summary + deep-link URL for a run",
		Tags:       []string{"agents"},
		Parameters: []openapi.Parameter{idParam},
	})
	b.Register("POST", "/api/v1/runs/{id}/cancel", openapi.RouteMeta{
		Summary:    "Cancel a run (proxied to AgentField)",
		Tags:       []string{"agents"},
		Parameters: []openapi.Parameter{idParam},
	})
	b.Register("POST", "/api/v1/runs/{id}/pause", openapi.RouteMeta{
		Summary:    "Pause a run (proxied to AgentField)",
		Tags:       []string{"agents"},
		Parameters: []openapi.Parameter{idParam},
	})
	b.Register("POST", "/api/v1/runs/{id}/resume", openapi.RouteMeta{
		Summary:    "Resume a paused run (proxied to AgentField)",
		Tags:       []string{"agents"},
		Parameters: []openapi.Parameter{idParam},
	})
	b.Register("POST", "/api/v1/runs/{id}/request-approval", openapi.RouteMeta{
		Summary:    "Request operator approval for a run (proxied to AgentField)",
		Tags:       []string{"agents"},
		Parameters: []openapi.Parameter{idParam},
	})
}

// registerSandboxOpenAPI describes /api/v1/sandbox/* routes.
func (s *Server) registerSandboxOpenAPI() {
	b := s.openapi
	b.AddTag("sandbox", "Sandboxed code execution")
	b.Register("POST", "/api/v1/sandbox/run", openapi.RouteMeta{
		Summary: "Run a sandboxed command (sync)", Tags: []string{"sandbox"},
	})
	b.Register("GET", "/api/v1/sandbox/runs", openapi.RouteMeta{
		Summary: "List sandbox runs", Tags: []string{"sandbox"},
		Parameters: []openapi.Parameter{
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "status", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "limit", In: "query", Schema: map[string]any{"type": "integer"}},
			{Name: "offset", In: "query", Schema: map[string]any{"type": "integer"}},
		},
	})
	b.Register("GET", "/api/v1/sandbox/runs/{id}", openapi.RouteMeta{
		Summary: "Get a single sandbox run", Tags: []string{"sandbox"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("DELETE", "/api/v1/sandbox/runs/{id}", openapi.RouteMeta{
		Summary: "Stop a running sandbox", Tags: []string{"sandbox"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("GET", "/api/v1/sandbox/pool", openapi.RouteMeta{
		Summary: "Sandbox pool stats + adapter capabilities", Tags: []string{"sandbox"},
	})
	b.Register("GET", "/api/v1/sandbox/runs/{id}/logs", openapi.RouteMeta{
		Summary: "SSE log stream from a running sandbox", Tags: []string{"sandbox"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
}
