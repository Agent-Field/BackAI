// SPDX-License-Identifier: Apache-2.0

// skills.go — REST handlers for the Phase 11.3 skills install/list/attach
// surface.
//
// Endpoints map 1:1 to SkillSchema / SkillListSchema /
// InstallSkillInputSchema / AttachSkillInputSchema in
// apps/dashboard/src/lib/api.ts. Any drift breaks the dashboard's
// safeParse — keep field names snake_case (the skills package already
// carries them), keep nullable fields emitted as JSON null (Go nil
// pointer), and keep arrays non-nil even when empty.
//
// The handlers delegate to skills.Store (persistence) and
// skills.Installer (manifest fetching). When the Store isn't wired
// (no DB at boot), reads return tolerant empty responses and mutations
// return 503 SKILLS_NOT_CONFIGURED.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/skills"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// installSkillInput mirrors InstallSkillInputSchema in api.ts. tenant_id
// is optional — when omitted the runtime installs the skill as a shared
// bundle (NULL tenant_id, visible to every tenant).
type installSkillInput struct {
	Source   string `json:"source"`
	TenantID string `json:"tenant_id,omitempty"`
}

// attachSkillInput mirrors AttachSkillInputSchema.
type attachSkillInput struct {
	SkillID string `json:"skill_id"`
	Agent   string `json:"agent"`
}

// skillListResponse mirrors SkillListSchema.
type skillListResponse struct {
	Skills []skills.Skill `json:"skills"`
}

// ─── Registration ─────────────────────────────────────────────────────────

func (s *Server) registerSkillsRoutes() {
	// S1b: skills bypass the tenant resolver (publicPrefixes) and honour an
	// attacker-controlled ?tenant= filter with no auth (reads AND unauthed
	// install/uninstall). Gate the whole surface to operators; the dashboard
	// manages skills via its better-auth session.
	g := func(h http.HandlerFunc) http.HandlerFunc { return s.operatorGuard(rbac.ResourceAdminSkills, h) }
	s.mux.HandleFunc("GET /api/v1/skills", g(s.handleListSkills))
	s.mux.HandleFunc("POST /api/v1/skills", g(s.handleInstallSkill))
	s.mux.HandleFunc("DELETE /api/v1/skills/{id}", g(s.handleUninstallSkill))
	s.mux.HandleFunc("POST /api/v1/skills/attach", g(s.handleAttachSkill))
}

// writeSkillsError maps skills package errors to HTTP responses.
func writeSkillsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, skills.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, skills.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "skill not found", nil)
	case errors.Is(err, skills.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable,
			"SKILLS_NOT_CONFIGURED",
			"skills module is not configured on this runtime", nil)
	case errors.Is(err, skills.ErrSourceUnreadable):
		writeError(w, http.StatusBadRequest, "SKILL_SOURCE_UNREADABLE", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── GET /api/v1/skills ───────────────────────────────────────────────────

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.skills.list")
	defer span.End()

	// Empty data when the store isn't wired. Mirrors the
	// notifications/billing pattern so the dashboard still renders.
	empty := skillListResponse{Skills: []skills.Skill{}}
	if s.skills == nil || !s.skills.HasPool() {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	q := r.URL.Query()
	filters := skills.ListFilters{
		TenantID: strings.TrimSpace(q.Get("tenant")),
		Harness:  strings.TrimSpace(q.Get("harness")),
	}
	out, err := s.skills.List(ctx, filters)
	if err != nil {
		writeSkillsError(w, err)
		return
	}
	if out == nil {
		out = []skills.Skill{}
	}
	writeJSON(w, http.StatusOK, skillListResponse{Skills: out})
}

// ─── POST /api/v1/skills ──────────────────────────────────────────────────

func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.skills.install")
	defer span.End()

	if s.skills == nil || !s.skills.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"SKILLS_NOT_CONFIGURED",
			"skills module is not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in installSkillInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	in.Source = strings.TrimSpace(in.Source)
	if in.Source == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"source is required", nil)
		return
	}

	installer := s.skillsInstaller
	if installer == nil {
		installer = skills.NewInstaller()
	}
	sk, err := installer.Install(in.Source, strings.TrimSpace(in.TenantID))
	if err != nil {
		writeSkillsError(w, err)
		return
	}
	out, err := s.skills.Install(ctx, sk)
	if err != nil {
		writeSkillsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// ─── DELETE /api/v1/skills/{id} ───────────────────────────────────────────

func (s *Server) handleUninstallSkill(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.skills.uninstall")
	defer span.End()

	if s.skills == nil || !s.skills.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"SKILLS_NOT_CONFIGURED",
			"skills module is not configured on this runtime", nil)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	if err := s.skills.Uninstall(ctx, id); err != nil {
		writeSkillsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── POST /api/v1/skills/attach ───────────────────────────────────────────

func (s *Server) handleAttachSkill(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.skills.attach")
	defer span.End()

	if s.skills == nil || !s.skills.HasPool() {
		writeError(w, http.StatusServiceUnavailable,
			"SKILLS_NOT_CONFIGURED",
			"skills module is not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in attachSkillInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	in.SkillID = strings.TrimSpace(in.SkillID)
	in.Agent = strings.TrimSpace(in.Agent)
	if in.SkillID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"skill_id is required", nil)
		return
	}
	if in.Agent == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"agent is required", nil)
		return
	}
	if err := s.skills.Attach(ctx, in.SkillID, in.Agent); err != nil {
		writeSkillsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"attached": true})
}

// ─── OpenAPI ──────────────────────────────────────────────────────────────

func (s *Server) registerSkillsOpenAPI() {
	b := s.openapi
	b.AddTag("skills", "AF skillkit bundles (install / list / attach)")
	b.Register("GET", "/api/v1/skills", openapi.RouteMeta{
		Summary: "List installed skills", Tags: []string{"skills"},
		Parameters: []openapi.Parameter{
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "harness", In: "query", Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/skills", openapi.RouteMeta{
		Summary: "Install a skill from a source string", Tags: []string{"skills"},
	})
	b.Register("DELETE", "/api/v1/skills/{id}", openapi.RouteMeta{
		Summary: "Uninstall a skill", Tags: []string{"skills"},
		Parameters: []openapi.Parameter{
			{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/skills/attach", openapi.RouteMeta{
		Summary: "Attach a skill onto an agent", Tags: []string{"skills"},
	})
}