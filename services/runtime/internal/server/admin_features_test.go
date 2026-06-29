// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/probe"
)

func TestAdminFeaturesEndpointIncludesProbeDetails(t *testing.T) {
	features, _ := config.PresetFeatures(config.PresetLean)
	probes := probe.NewRegistry(nil)
	probes.StoreResult(probe.Result{
		ProbeID:    probe.LiteLLMVirtualKeysProbeID,
		Capability: "llm_gateway.virtual_keys_active",
		Value:      false,
		Severity:   probe.SeverityUnavailable,
		Detail:     "LiteLLM in stateless mode",
		LastRun:    time.Now().UTC(),
	})
	s := New(config.Default(), nil, Deps{
		FeatureConfig: config.FeatureConfig{Preset: config.PresetLean, Features: features},
		ProbeRegistry: probes,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/features", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var out adminFeaturesResponse
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	llm := out.Features["llm_gateway"]
	if llm.CapabilityStatus != featureStatusDegraded {
		t.Fatalf("llm status = %s", llm.CapabilityStatus)
	}
	if len(llm.Details) != 1 || llm.Details[0].Key != "virtual_keys_active" {
		t.Fatalf("details = %+v", llm.Details)
	}
}

func TestAdminFeaturesEndpointIncludesValidatorWarnings(t *testing.T) {
	features, _ := config.PresetFeatures(config.PresetLean)
	s := New(config.Default(), nil, Deps{
		FeatureConfig: config.FeatureConfig{Preset: config.PresetLean, Features: features},
		FeatureWarnings: []config.ValidationError{{
			Feature:     "metrics.container_metrics",
			Level:       config.ValidationErrorLevel,
			Message:     "requires metrics.enabled",
			Remediation: "enable metrics",
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/features", nil)
	rr := httptest.NewRecorder()
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var out adminFeaturesResponse
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.ValidatorWarnings) != 1 {
		t.Fatalf("warnings = %+v", out.ValidatorWarnings)
	}
}

func TestAdminFeaturesOpenAPIRegistered(t *testing.T) {
	s := New(config.Default(), nil, Deps{})
	spec := s.openapi.Build()
	if _, ok := spec.Paths["/api/v1/admin/features"]; !ok {
		t.Fatalf("/api/v1/admin/features missing from OpenAPI paths")
	}
}
