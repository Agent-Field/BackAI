// SPDX-License-Identifier: Apache-2.0

// storage_isolation_test.go — S8: object storage must be tenant-isolated
// when multi-tenancy is on. One tenant must never list, read, or delete
// another tenant's objects. These tests drive the handlers with an
// injected tenant context (the resolver middleware isn't in the raw-mux
// path) and assert the physical keys + visible listings.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// newMTStorageServer builds a storage server with multi-tenancy enabled.
func newMTStorageServer(t *testing.T, store storage.Storage, prefix string) *Server {
	t.Helper()
	cfg := config.Default()
	if cfg.Modules.Enabled == nil {
		cfg.Modules.Enabled = map[string]bool{}
	}
	cfg.Modules.Enabled["multi-tenancy"] = true
	return New(cfg, slog.Default(), Deps{Storage: store, StoragePrefix: prefix})
}

// asTenant attaches a resolved tenant to the request context (what the
// tenant resolver would do for a valid API key).
func asTenant(req *http.Request, tenantID string) *http.Request {
	return req.WithContext(tenantctx.WithTenantAndUser(req.Context(), tenantID, "", ""))
}

// C1: two tenants uploading the same caller key land in separate physical
// namespaces and never collide.
func TestStorageUploadIsolatesTenants(t *testing.T) {
	store := newStubStorage()
	srv := newMTStorageServer(t, store, "")

	for _, tc := range []struct{ tenant, body string }{
		{"tenant-a", "A-content"},
		{"tenant-b", "B-content"},
	} {
		req := asTenant(uploadRequest(t, "report.pdf", "report.pdf", "application/pdf", tc.body), tc.tenant)
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s upload: %d %s", tc.tenant, rec.Code, rec.Body.String())
		}
		// The wire key the caller sees is clean (prefix stripped).
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out["key"] != "report.pdf" {
			t.Fatalf("%s should see clean key, got %v", tc.tenant, out["key"])
		}
	}

	// Physically stored under distinct tenant prefixes — no overwrite.
	if _, ok := store.objects["tenants/tenant-a/report.pdf"]; !ok {
		t.Fatalf("tenant-a object not under its prefix: %v", stubKeys(store))
	}
	if _, ok := store.objects["tenants/tenant-b/report.pdf"]; !ok {
		t.Fatalf("tenant-b object not under its prefix: %v", stubKeys(store))
	}
	if string(store.objects["tenants/tenant-a/report.pdf"]) != "A-content" {
		t.Fatal("tenant-b overwrote tenant-a's object — isolation broken")
	}
}

// C5: a bare list returns ONLY the caller tenant's objects, with clean keys.
func TestStorageListConfinedToTenant(t *testing.T) {
	store := newStubStorage()
	// Seed objects for two tenants + a legacy unprefixed object.
	store.objects["tenants/tenant-a/a1.txt"] = []byte("a1")
	store.objects["tenants/tenant-a/a2.txt"] = []byte("a2")
	store.objects["tenants/tenant-b/b1.txt"] = []byte("b1")
	store.objects["sandbox/runs/leak.log"] = []byte("legacy") // pre-existing, unprefixed
	srv := newMTStorageServer(t, store, "")

	req := asTenant(httptest.NewRequest("GET", "/api/v1/storage", nil), "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Objects []struct {
			Key string `json:"key"`
		} `json:"objects"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	got := map[string]bool{}
	for _, o := range out.Objects {
		got[o.Key] = true
	}
	if !got["a1.txt"] || !got["a2.txt"] {
		t.Fatalf("tenant-a should see its own keys, got %v", got)
	}
	if got["b1.txt"] {
		t.Fatal("cross-tenant leak: tenant-a saw tenant-b's object")
	}
	for k := range got {
		if strings.Contains(k, "sandbox/runs/leak.log") || strings.Contains(k, "tenant-b") {
			t.Fatalf("tenant-a saw a foreign/legacy key: %q", k)
		}
	}
}

// C1 (read path): a tenant cannot download another tenant's object.
func TestStorageDownloadCrossTenantDenied(t *testing.T) {
	store := newStubStorage()
	store.objects["tenants/tenant-a/secret.txt"] = []byte("top-secret")
	srv := newMTStorageServer(t, store, "")

	// tenant-b asks for the same caller key -> resolves to its own empty
	// namespace -> 404, never tenant-a's bytes.
	reqB := asTenant(httptest.NewRequest("GET", "/api/v1/storage/secret.txt", nil), "tenant-b")
	recB := httptest.NewRecorder()
	srv.mux.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusNotFound {
		t.Fatalf("tenant-b should get 404, got %d %s", recB.Code, recB.Body.String())
	}

	// tenant-a gets its own object.
	reqA := asTenant(httptest.NewRequest("GET", "/api/v1/storage/secret.txt", nil), "tenant-a")
	recA := httptest.NewRecorder()
	srv.mux.ServeHTTP(recA, reqA)
	if recA.Code != http.StatusOK || recA.Body.String() != "top-secret" {
		t.Fatalf("tenant-a should read its object, got %d %s", recA.Code, recA.Body.String())
	}
}

// C1 (delete path): a tenant's delete cannot touch another tenant's object.
func TestStorageDeleteCrossTenantDenied(t *testing.T) {
	store := newStubStorage()
	store.objects["tenants/tenant-a/keep.txt"] = []byte("keep")
	srv := newMTStorageServer(t, store, "")

	reqB := asTenant(httptest.NewRequest("DELETE", "/api/v1/storage/keep.txt", nil), "tenant-b")
	recB := httptest.NewRecorder()
	srv.mux.ServeHTTP(recB, reqB)
	if recB.Code != http.StatusOK { // idempotent success on B's (empty) namespace
		t.Fatalf("tenant-b delete: %d %s", recB.Code, recB.Body.String())
	}
	if _, ok := store.objects["tenants/tenant-a/keep.txt"]; !ok {
		t.Fatal("cross-tenant delete removed tenant-a's object — isolation broken")
	}
}

// C2: with multi-tenancy OFF, keys pass through unchanged (single-tenant
// back-compat — no accidental tenant prefixing).
func TestStorageNoTenantPrefixWhenMTOff(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "") // MT off (config.Default)
	req := uploadRequest(t, "plain.txt", "plain.txt", "text/plain", "hi")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := store.objects["plain.txt"]; !ok {
		t.Fatalf("MT off should store the raw key, got %v", stubKeys(store))
	}
}

// C3 + C4: pure helpers — traversal-safe sanitisation and prefix symmetry.
func TestStoragePrefixHelpers(t *testing.T) {
	// sanitizeTenantSegment strips anything that could break out of the
	// namespace.
	for in, want := range map[string]string{
		"7c9e-uuid-1234":  "7c9e-uuid-1234",
		"../../etc/passwd": "etcpasswd",
		"a/b/c":            "abc",
		"t.\x00e/n":        "ten",
	} {
		if got := sanitizeTenantSegment(in); got != want {
			t.Errorf("sanitizeTenantSegment(%q) = %q, want %q", in, got, want)
		}
	}
	// applyPrefix / stripPrefix round-trip.
	for _, prefix := range []string{"", "tenants/t1", "base/tenants/t1/"} {
		key := "docs/readme.md"
		full := applyPrefix(prefix, key)
		if got := stripPrefix(prefix, full); got != key {
			t.Errorf("round-trip prefix=%q: got %q, want %q", prefix, got, key)
		}
	}
}

func stubKeys(s *stubStorage) []string {
	out := make([]string, 0, len(s.objects))
	for k := range s.objects {
		out = append(out, k)
	}
	return out
}
