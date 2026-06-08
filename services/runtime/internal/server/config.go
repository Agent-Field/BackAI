// SPDX-License-Identifier: Apache-2.0

// Runtime configuration endpoints. Today this owns feature flags; keep
// app/product config here and leave AgentField state in AgentField.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/featureflags"
)

func (s *Server) registerConfigRoutes() {
	s.mux.HandleFunc("GET /api/v1/config/flags", s.handleListFeatureFlags)
	s.mux.HandleFunc("PUT /api/v1/config/flags/{key}", s.handleSetFeatureFlag)
}

func writeFeatureFlagError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, featureflags.ErrTenantRequired):
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED", "tenant context required", nil)
	case errors.Is(err, featureflags.ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

func (s *Server) handleListFeatureFlags(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.config.flags.list")
	defer span.End()

	if s.featureFlags == nil {
		writeJSON(w, http.StatusOK, featureflags.List{Flags: featureflags.Defaults()})
		return
	}
	flags, err := s.featureFlags.List(ctx)
	if err != nil {
		span.RecordError(err)
		writeFeatureFlagError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flags)
}

type setFeatureFlagInput struct {
	Enabled     bool           `json:"enabled"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (s *Server) handleSetFeatureFlag(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.config.flags.set")
	defer span.End()
	if s.featureFlags == nil {
		writeError(w, http.StatusServiceUnavailable, "FEATURE_FLAGS_NOT_CONFIGURED",
			"feature flag persistence is not configured on this runtime", nil)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in setFeatureFlagInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"invalid JSON body: "+err.Error(), nil)
		return
	}
	key := strings.TrimSpace(r.PathValue("key"))
	flag, err := s.featureFlags.Set(ctx, key, featureflags.SetInput{
		Enabled:     in.Enabled,
		Label:       in.Label,
		Description: in.Description,
		Metadata:    in.Metadata,
	})
	if err != nil {
		span.RecordError(err)
		writeFeatureFlagError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flag)
}
