// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

func TestAdminAdaptersEndpointReturnsRegistry(t *testing.T) {
	reg := adapterregistry.New()
	reg.Register(adapterregistry.Slot{
		ID:           "sandbox",
		Tier:         adapterregistry.Tier1,
		Kind:         adapterregistry.KindBuiltin,
		Name:         "docker",
		Capabilities: json.RawMessage(`{"supports_gpu":false}`),
		SwapMethod:   "env_var",
		SwapEnv:      "AF_STACK_SANDBOX_ADAPTER",
	})
	srv := New(config.Default(), slog.Default(), Deps{AdapterRegistry: reg})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/adapters", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out adapterregistry.AdaptersResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 1 {
		t.Fatalf("slots = %d, want 1", len(out.Slots))
	}
	if got := out.Slots[0].Slot; got != "sandbox" {
		t.Fatalf("slot = %q, want sandbox", got)
	}
	if got := out.Slots[0].Active.Name; got != "docker" {
		t.Fatalf("active.name = %q, want docker", got)
	}
}

func TestAdminAdaptersEndpointDegradesToEmptyList(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/adapters", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out adapterregistry.AdaptersResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Slots) != 0 {
		t.Fatalf("slots = %d, want 0", len(out.Slots))
	}
}

func TestAdminAdaptersAppearsInOpenAPI(t *testing.T) {
	srv := New(config.Default(), slog.Default(), Deps{})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&spec); err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Paths["/api/v1/admin/adapters"]; !ok {
		t.Fatalf("/api/v1/admin/adapters missing from OpenAPI paths")
	}
}
