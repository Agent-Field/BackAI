// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

func (s *Server) registerAdminAdapterRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/adapters", s.handleAdminListAdapters)
}

func (s *Server) handleAdminListAdapters(w http.ResponseWriter, r *http.Request) {
	if s.adapterRegistry == nil {
		writeJSON(w, http.StatusOK, adapterregistry.AdaptersResponse{Slots: []adapterregistry.SlotView{}})
		return
	}
	writeJSON(w, http.StatusOK, s.adapterRegistry.List(r.Context()))
}

func (s *Server) registerAdminAdapterOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/adapters", openapi.RouteMeta{
		Summary:       "List configured adapter slots and capabilities",
		Tags:          []string{"admin"},
		OkDescription: "Adapter slot inventory",
	})
}
