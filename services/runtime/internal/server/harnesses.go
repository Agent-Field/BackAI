// harnesses.go — REST handlers for Phase 11.4 harness probes.
//
// Endpoints map 1:1 to HarnessSchema / HarnessListSchema in
// apps/dashboard/src/lib/api.ts. Any drift breaks the dashboard's
// safeParse — keep field names snake_case (the harnesses package
// already carries them via JSON tags), keep nullable fields emitted as
// JSON null (Go nil pointer), and keep required_env non-nil even when
// empty.
//
// All three endpoints are read-mostly; POST /probe is the only one that
// performs work (re-runs the probe). The handler delegates to
// harnesses.Service which caches the last probe result in-memory per
// provider, so the GET list path stays cheap even at high request
// rates.
package server

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/backai/services/runtime/internal/harnesses"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

// harnessListResponse mirrors HarnessListSchema in api.ts.
type harnessListResponse struct {
	Harnesses []harnesses.Harness `json:"harnesses"`
}

// ─── Registration ────────────────────────────────────────────────────────

func (s *Server) registerHarnessesRoutes() {
	s.mux.HandleFunc("GET /api/v1/harnesses", s.handleListHarnesses)
	s.mux.HandleFunc("GET /api/v1/harnesses/{provider}", s.handleGetHarness)
	s.mux.HandleFunc("POST /api/v1/harnesses/{provider}/probe", s.handleProbeHarness)
}

func writeHarnessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, harnesses.ErrUnknownProvider):
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"unknown harness provider; want claude-code|codex|gemini|opencode", nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── GET /api/v1/harnesses ───────────────────────────────────────────────

func (s *Server) handleListHarnesses(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.harnesses.list")
	defer span.End()

	// When no service is wired (defensive — main.go always constructs
	// one) emit an empty list so the dashboard renders the empty state.
	resp := harnessListResponse{Harnesses: []harnesses.Harness{}}
	if s.harnesses == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Harnesses = s.harnesses.ListAll(ctx)
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/harnesses/{provider} ────────────────────────────────────

func (s *Server) handleGetHarness(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.harnesses.get")
	defer span.End()
	if s.harnesses == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND",
			"harness probes not configured on this runtime", nil)
		return
	}
	provider := harnesses.Provider(r.PathValue("provider"))
	h, err := s.harnesses.Get(ctx, provider)
	if err != nil {
		writeHarnessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// ─── POST /api/v1/harnesses/{provider}/probe ─────────────────────────────

func (s *Server) handleProbeHarness(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.harnesses.probe")
	defer span.End()
	if s.harnesses == nil {
		writeError(w, http.StatusServiceUnavailable,
			"HARNESSES_NOT_CONFIGURED",
			"harness probes not configured on this runtime", nil)
		return
	}
	provider := harnesses.Provider(r.PathValue("provider"))
	h, err := s.harnesses.Probe(ctx, provider)
	if err != nil {
		writeHarnessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// ─── OpenAPI ─────────────────────────────────────────────────────────────

func (s *Server) registerHarnessesOpenAPI() {
	b := s.openapi
	b.AddTag("harnesses",
		"CLI agent harness probes (Claude Code, Codex, Gemini, OpenCode)")
	b.Register("GET", "/api/v1/harnesses", openapi.RouteMeta{
		Summary: "List harness install + auth status", Tags: []string{"harnesses"},
	})
	b.Register("GET", "/api/v1/harnesses/{provider}", openapi.RouteMeta{
		Summary: "Get the cached probe result for one harness",
		Tags:    []string{"harnesses"},
		Parameters: []openapi.Parameter{
			{Name: "provider", In: "path", Required: true,
				Description: "claude-code|codex|gemini|opencode",
				Schema:      map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/harnesses/{provider}/probe", openapi.RouteMeta{
		Summary: "Re-probe a harness and return the fresh result",
		Tags:    []string{"harnesses"},
		Parameters: []openapi.Parameter{
			{Name: "provider", In: "path", Required: true,
				Description: "claude-code|codex|gemini|opencode",
				Schema:      map[string]any{"type": "string"}},
		},
	})
}
