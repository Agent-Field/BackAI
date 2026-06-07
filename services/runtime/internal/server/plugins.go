// SPDX-License-Identifier: Apache-2.0

// plugins.go — REST handler for the Phase 12.3 plugin manifest endpoint.
//
// Endpoint (path + response shape match apps/dashboard/src/lib/api.ts):
//
//	GET /api/v1/plugins -> PluginList   { plugins: Plugin[] }
//
// Design note (Phase 12.3): dashboard plugins are TypeScript files
// discovered AT BUILD TIME of the dashboard (see
// apps/dashboard/scripts/generate-plugins-manifest.mjs). The runtime
// does not — and intentionally cannot — see those source files. To keep
// a single source of truth without coupling the runtime to the
// dashboard's build artefacts, this endpoint returns an empty list.
//
// The dashboard client merges the runtime response with its own
// build-generated PLUGINS constant (see apps/dashboard/src/lib/plugins.ts:
// loadPluginManifest). End result: the OpenAPI contract is satisfied,
// every consumer of /api/v1/plugins gets a valid (empty) PluginList, and
// the dashboard renders the full plugin set without a server roundtrip.
//
// If a future deployment wants server-authoritative plugins (e.g. so a
// non-dashboard client can list them), this handler is the place to
// wire that — load a JSON manifest from disk or DB and return it here.

package server

import (
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// pluginWire mirrors PluginSchema in apps/dashboard/src/lib/api.ts.
type pluginWire struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Route       string `json:"route"`
	Icon        string `json:"icon"`
	Group       string `json:"group"`
	Version     string `json:"version"`
}

// pluginListWire mirrors PluginListSchema.
type pluginListWire struct {
	Plugins []pluginWire `json:"plugins"`
}

// registerPluginsRoutes wires the plugin manifest endpoint.
func (s *Server) registerPluginsRoutes() {
	s.mux.HandleFunc("GET /api/v1/plugins", s.handleListPlugins)
}

func (s *Server) registerPluginsOpenAPI() {
	if s.openapi == nil {
		return
	}
	s.openapi.Register("GET", "/api/v1/plugins", openapi.RouteMeta{
		Summary: "List dashboard plugins discovered at build time",
		Tags:    []string{"dashboard"},
		OkDescription: "Plugin manifest list — runtime returns an empty " +
			"list; the dashboard merges its own build-time discovered " +
			"plugins client-side.",
	})
}

// handleListPlugins implements GET /api/v1/plugins.
//
// Returns an empty PluginList. See file-level comment for rationale.
func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.plugins.list")
	defer span.End()
	writeJSON(w, http.StatusOK, pluginListWire{Plugins: []pluginWire{}})
}