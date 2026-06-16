// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

func (s *Server) registerLLMProviderHealthRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/llm/provider-health", s.handleLLMProviderHealth)
}

func (s *Server) registerLLMProviderHealthOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/llm/provider-health", openapi.RouteMeta{
		Summary: "LLM provider availability rollup", Tags: []string{"admin", "llm"},
		Parameters: []openapi.Parameter{
			{Name: "window", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
}

func (s *Server) handleLLMProviderHealth(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.Pool == nil {
		writeJSON(w, http.StatusOK, llmgateway.ProviderHealthList{Providers: []llmgateway.ProviderHealthSnapshot{}, Window: "24h"})
		return
	}
	window := parseProviderHealthWindow(strings.TrimSpace(r.URL.Query().Get("window")))
	out, err := llmgateway.ReadProviderHealth(r.Context(), s.db.Pool, window)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func parseProviderHealthWindow(raw string) time.Duration {
	if raw == "" {
		return 24 * time.Hour
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	switch raw {
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
