// SPDX-License-Identifier: Apache-2.0

// Workload modules (PRD R2). Filesystem-discovered declarative modules
// mount tenant-scoped CRUD under /api/v1/workload/<module>/<resource>.
// The heavy lifting — discovery, manifest validation, migration
// application, route generation — lives in internal/modules. This file is
// only the server-side wiring: it hands the manager the runtime's pool +
// canonical response writers, mounts its routes, and exposes the operator
// inventory endpoint.
package server

import (
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/modules"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// resourceAdminModules is matched by the admin:* Casbin policy (owner:
// read|write|delete, admin: read), so no rbac change is needed to gate it.
const resourceAdminModules = "admin:modules"

// registerWorkloadModuleRoutes wires the workload-module surface. When no
// manager is configured (no modules discovered / not wired at boot) the
// admin endpoint still serves an empty inventory so the dashboard renders.
func (s *Server) registerWorkloadModuleRoutes() {
	if s.modules != nil {
		s.modules.SetResponder(modules.Responder{
			JSON:  writeJSON,
			Error: writeError,
		})
		if s.db != nil && s.db.Pool != nil {
			s.modules.SetDB(s.db.Pool)
		}
		s.modules.Mount(s.mux, s.openapi)
	}

	s.mux.HandleFunc("GET /api/v1/admin/modules",
		s.operatorGuard(resourceAdminModules, s.handleAdminWorkloadModules))
	s.openapi.Register("GET", "/api/v1/admin/modules", openapi.RouteMeta{
		Summary: "List discovered workload modules with health + migration state",
		Tags:    []string{"admin"},
	})
}

// handleAdminWorkloadModules returns the operator inventory of discovered
// workload modules: id/version/health/migration state per module.
func (s *Server) handleAdminWorkloadModules(w http.ResponseWriter, r *http.Request) {
	_, span := s.dashTracer().Start(r.Context(), "dashboard.modules.workload_list")
	defer span.End()

	modulesList := []modules.AdminModuleView{}
	if s.modules != nil {
		modulesList = s.modules.AdminList()
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": modulesList})
}
