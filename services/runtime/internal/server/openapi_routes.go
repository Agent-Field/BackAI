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

// NOTE: registerAdminOpenAPI lives in admin.go (alongside the handler
// declarations). It used to be here, but co-locating it with the
// handlers makes future param/response schema edits easier.
