// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type brandResponse struct {
	Brand         map[string]any `json:"brand"`
	Override      map[string]any `json:"override,omitempty"`
	Source        string         `json:"source"`
	UpdatedAt     *string        `json:"updated_at"`
	Apply         string         `json:"apply"`
	BrandYAMLPath string         `json:"brand_yaml_path"`
}

type brandUpdateInput struct {
	Brand map[string]any `json:"brand"`
}

func (s *Server) registerAdminBrandRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/brand", s.handleAdminGetBrand)
	s.mux.HandleFunc("PUT /api/v1/admin/brand", s.handleAdminPutBrand)
	s.mux.HandleFunc("DELETE /api/v1/admin/brand", s.handleAdminDeleteBrand)
}

func (s *Server) registerAdminBrandOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/brand", openapi.RouteMeta{
		Summary: "Read brand.yaml plus runtime override", Tags: []string{"admin"},
	})
	s.openapi.Register("PUT", "/api/v1/admin/brand", openapi.RouteMeta{
		Summary: "Update runtime brand override", Tags: []string{"admin"},
	})
	s.openapi.Register("DELETE", "/api/v1/admin/brand", openapi.RouteMeta{
		Summary: "Reset runtime brand override", Tags: []string{"admin"},
	})
}

func (s *Server) handleAdminGetBrand(w http.ResponseWriter, r *http.Request) {
	base, path, err := readBrandYAML()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "BRAND_READ_FAILED", err.Error(), nil)
		return
	}
	override, updatedAt, err := s.readBrandOverride(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "BRAND_OVERRIDE_READ_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, brandResponse{
		Brand:         mergeBrand(base, override),
		Override:      override,
		Source:        "brand.yaml+db_override",
		UpdatedAt:     updatedAt,
		Apply:         "Restart or redeploy the customer-app for build-time surfaces to pick up the override.",
		BrandYAMLPath: path,
	})
}

func (s *Server) handleAdminPutBrand(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_NOT_CONFIGURED", "database is required to store brand override", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in brandUpdateInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	if len(in.Brand) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "brand object is required", nil)
		return
	}
	override := sanitizeBrandOverride(in.Brand)
	payload, err := json.Marshal(override)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "brand object must be JSON serializable", nil)
		return
	}
	var updatedAt time.Time
	if err := s.db.Pool.QueryRow(r.Context(), `
		insert into suite_brand_override (id, brand, updated_by, updated_at)
		values (true, $1::jsonb, null, now())
		on conflict (id) do update set
		  brand = excluded.brand,
		  updated_by = excluded.updated_by,
		  updated_at = now()
		returning updated_at
	`, payload).Scan(&updatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "BRAND_OVERRIDE_WRITE_FAILED", err.Error(), nil)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "brand.update",
		ResourceType: "brand",
		ResourceID:   "runtime",
		Metadata: map[string]any{
			"keys": mapKeys(override),
		},
	})
	base, path, _ := readBrandYAML()
	updated := updatedAt.UTC().Format(time.RFC3339Nano)
	writeJSON(w, http.StatusOK, brandResponse{
		Brand:         mergeBrand(base, override),
		Override:      override,
		Source:        "brand.yaml+db_override",
		UpdatedAt:     &updated,
		Apply:         "Restart or redeploy the customer-app for build-time surfaces to pick up the override.",
		BrandYAMLPath: path,
	})
}

func (s *Server) handleAdminDeleteBrand(w http.ResponseWriter, r *http.Request) {
	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_NOT_CONFIGURED", "database is required to reset brand override", nil)
		return
	}
	if _, err := s.db.Pool.Exec(r.Context(), `delete from suite_brand_override where id = true`); err != nil {
		writeError(w, http.StatusInternalServerError, "BRAND_OVERRIDE_DELETE_FAILED", err.Error(), nil)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "brand.reset",
		ResourceType: "brand",
		ResourceID:   "runtime",
	})
	base, path, err := readBrandYAML()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "BRAND_READ_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, brandResponse{
		Brand:         base,
		Override:      nil,
		Source:        "brand.yaml",
		UpdatedAt:     nil,
		Apply:         "Restart or redeploy the customer-app for build-time surfaces to pick up the reset.",
		BrandYAMLPath: path,
	})
}

func readBrandYAML() (map[string]any, string, error) {
	for _, p := range []string{"brand.yaml", "../../brand.yaml", "/app/brand.yaml"} {
		abs, _ := filepath.Abs(p)
		data, err := os.ReadFile(p)
		if err == nil {
			var out map[string]any
			if err := yaml.Unmarshal(data, &out); err != nil {
				return nil, abs, err
			}
			if out == nil {
				out = map[string]any{}
			}
			return out, abs, nil
		}
	}
	return nil, "", errors.New("brand.yaml not found")
}

func (s *Server) readBrandOverride(ctx context.Context) (map[string]any, *string, error) {
	if s.db == nil || s.db.Pool == nil {
		return nil, nil, nil
	}
	var (
		raw       []byte
		updatedAt time.Time
	)
	if err := s.db.Pool.QueryRow(ctx, `
		select brand, updated_at from suite_brand_override where id = true
	`).Scan(&raw, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	updated := updatedAt.UTC().Format(time.RFC3339Nano)
	return out, &updated, nil
}

func mergeBrand(base, override map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if v == nil {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeBrandOverride(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if v == nil {
			continue
		}
		out[k] = v
	}
	return out
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
