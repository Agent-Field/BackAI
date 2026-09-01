// SPDX-License-Identifier: Apache-2.0

// Package server — tenant secrets handler tests.
//
// These exercise the /api/v1/vault/secrets/* surface without a DB by
// injecting a fake secrets.Store keyed by (tenant, key). The fake
// isolates by tenant exactly as the real vault's WHERE tenant_id +
// FORCE-RLS policy do, so the handler-level isolation contract is fully
// testable here. The DB/RLS-level isolation (that Postgres itself rejects
// a cross-tenant row even if a handler passed the wrong id) is asserted by
// the secrets package + migration tests and is skipped here (no DB).
//
// The tenant surface rides the tenant resolver in production; these tests
// call s.mux directly (which does not run the resolver middleware) and set
// the resolved tenant on the request context by hand, mirroring what the
// resolver produces for a valid bearer key.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// ─── fake store ────────────────────────────────────────────────────────────

type fakeSecretStore struct {
	mu   sync.Mutex
	data map[string]map[string][]byte // tenant -> key -> plaintext
}

func newFakeSecretStore() *fakeSecretStore {
	return &fakeSecretStore{data: map[string]map[string][]byte{}}
}

func (f *fakeSecretStore) meta(tenantID, key string) secrets.SecretMetadata {
	tid := tenantID
	return secrets.SecretMetadata{
		Key:       key,
		TenantID:  &tid,
		CreatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339Nano),
		UpdatedAt: time.Unix(0, 0).UTC().Format(time.RFC3339Nano),
	}
}

func (f *fakeSecretStore) Get(_ context.Context, tenantID, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.data[tenantID][key]; ok {
		return v, nil
	}
	return nil, secrets.ErrSecretNotFound
}

func (f *fakeSecretStore) GetMetadata(_ context.Context, tenantID, key string) (secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[tenantID][key]; ok {
		return f.meta(tenantID, key), nil
	}
	return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
}

func (f *fakeSecretStore) Put(_ context.Context, tenantID, key string, in secrets.PutInput) (secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data[tenantID] == nil {
		f.data[tenantID] = map[string][]byte{}
	}
	f.data[tenantID][key] = []byte(in.Value)
	return f.meta(tenantID, key), nil
}

func (f *fakeSecretStore) Delete(_ context.Context, tenantID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[tenantID][key]; !ok {
		return secrets.ErrSecretNotFound
	}
	delete(f.data[tenantID], key)
	return nil
}

func (f *fakeSecretStore) List(_ context.Context, tenantID string) ([]secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []secrets.SecretMetadata{}
	for key := range f.data[tenantID] {
		out = append(out, f.meta(tenantID, key))
	}
	return out, nil
}

func (f *fakeSecretStore) Rotate(_ context.Context, tenantID, key, newValue string) (secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[tenantID][key]; !ok {
		return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	f.data[tenantID][key] = []byte(newValue)
	return f.meta(tenantID, key), nil
}

var _ secrets.Store = (*fakeSecretStore)(nil)

// ─── helpers ───────────────────────────────────────────────────────────────

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

// tenantSecretReq builds a request whose context carries the resolved
// tenant, mirroring what tenantResolver attaches for a valid bearer key.
// It reuses asTenant (storage_isolation_test.go) for the context binding.
func tenantSecretReq(method, path, tenantID, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return asTenant(r, tenantID)
}

func newTenantSecretsServer(t *testing.T, store secrets.Store) *Server {
	t.Helper()
	s := newBareTestServer(t)
	s.secretStore = store
	return s
}

// ─── contract: set / list / get / delete round-trip, no plaintext ──────────

func TestTenantSecretsSetListGetDeleteRoundTrip(t *testing.T) {
	s := newTenantSecretsServer(t, newFakeSecretStore())

	// set
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("PUT", "/api/v1/vault/secrets/OPENAI_API_KEY", tenantA,
		`{"value":"sk-secret","description":"prod"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("put: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// list — metadata only, includes reference, NEVER the plaintext value
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("GET", "/api/v1/vault/secrets", tenantA, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("list leaked plaintext: %s", rec.Body.String())
	}
	var listed tenantSecretListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(listed.Secrets) != 1 || listed.Secrets[0].Key != "OPENAI_API_KEY" {
		t.Fatalf("list: unexpected rows: %+v", listed.Secrets)
	}
	if listed.Secrets[0].Reference != "secret:OPENAI_API_KEY" {
		t.Fatalf("list: want reference secret:OPENAI_API_KEY, got %q", listed.Secrets[0].Reference)
	}

	// get-metadata — no plaintext, carries reference
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("GET", "/api/v1/vault/secrets/OPENAI_API_KEY", tenantA, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sk-secret") {
		t.Fatalf("get leaked plaintext: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"reference":"secret:OPENAI_API_KEY"`) {
		t.Fatalf("get missing reference: %s", rec.Body.String())
	}

	// reveal — the explicit plaintext contract
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/OPENAI_API_KEY/reveal", tenantA, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("reveal: want 200, got %d", rec.Code)
	}
	var revealed secretValueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &revealed); err != nil {
		t.Fatalf("reveal body: %v", err)
	}
	if revealed.Value != "sk-secret" {
		t.Fatalf("reveal: want sk-secret, got %q", revealed.Value)
	}

	// delete
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("DELETE", "/api/v1/vault/secrets/OPENAI_API_KEY", tenantA, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: want 200, got %d", rec.Code)
	}

	// list is now empty
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("GET", "/api/v1/vault/secrets", tenantA, ""))
	var after tenantSecretListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if len(after.Secrets) != 0 {
		t.Fatalf("after delete: want 0 rows, got %+v", after.Secrets)
	}
}

// ─── contract: tenant isolation ────────────────────────────────────────────

func TestTenantSecretsAreIsolatedAcrossTenants(t *testing.T) {
	s := newTenantSecretsServer(t, newFakeSecretStore())

	// Tenant A writes a secret named "shared".
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("PUT", "/api/v1/vault/secrets/shared", tenantA,
		`{"value":"A-only"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("A put: want 200, got %d", rec.Code)
	}

	// Tenant B lists — must NOT see A's secret.
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("GET", "/api/v1/vault/secrets", tenantB, ""))
	var bList tenantSecretListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &bList)
	if len(bList.Secrets) != 0 {
		t.Fatalf("tenant B saw tenant A's secrets: %+v", bList.Secrets)
	}

	// Tenant B revealing the same key gets 404, never A's plaintext.
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/shared/reveal", tenantB, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tenant B reveal of A's key: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "A-only") {
		t.Fatalf("tenant B reveal leaked A's plaintext: %s", rec.Body.String())
	}

	// Tenant B may store its OWN key of the same name independently.
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("PUT", "/api/v1/vault/secrets/shared", tenantB,
		`{"value":"B-only"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("B put: want 200, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/shared/reveal", tenantB, ""))
	var bReveal secretValueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &bReveal)
	if bReveal.Value != "B-only" {
		t.Fatalf("tenant B reveal: want B-only, got %q", bReveal.Value)
	}
	// And A still sees its own value, unchanged by B's write.
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/shared/reveal", tenantA, ""))
	var aReveal secretValueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &aReveal)
	if aReveal.Value != "A-only" {
		t.Fatalf("tenant A reveal after B write: want A-only, got %q", aReveal.Value)
	}
}

// ─── contract: rotate is scoped + requires an existing key ─────────────────

func TestTenantSecretsRotate(t *testing.T) {
	s := newTenantSecretsServer(t, newFakeSecretStore())

	// rotate before create -> 404
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/k/rotate", tenantA, `{"value":"v1"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("rotate missing: want 404, got %d", rec.Code)
	}

	// create then rotate
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("PUT", "/api/v1/vault/secrets/k", tenantA, `{"value":"v1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("put: want 200, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/k/rotate", tenantA, `{"value":"v2"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, tenantSecretReq("POST", "/api/v1/vault/secrets/k/reveal", tenantA, ""))
	var rev secretValueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rev)
	if rev.Value != "v2" {
		t.Fatalf("after rotate: want v2, got %q", rev.Value)
	}
}

// ─── contract: 503 when no vault is wired ──────────────────────────────────

func TestTenantSecretsReturn503WhenVaultMissing(t *testing.T) {
	s := newBareTestServer(t) // no store injected, no vault
	cases := []struct{ method, path, body string }{
		{"GET", "/api/v1/vault/secrets", ""},
		{"GET", "/api/v1/vault/secrets/k", ""},
		{"PUT", "/api/v1/vault/secrets/k", `{"value":"v"}`},
		{"DELETE", "/api/v1/vault/secrets/k", ""},
		{"POST", "/api/v1/vault/secrets/k/reveal", ""},
		{"POST", "/api/v1/vault/secrets/k/rotate", `{"value":"v"}`},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, tenantSecretReq(c.method, c.path, tenantA, c.body))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("want 503, got %d body=%s", rec.Code, rec.Body.String())
			}
			assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
		})
	}
}

// ─── contract: tenant-less context is refused (defence-in-depth) ───────────

func TestTenantSecretsDenyWithoutResolvedTenant(t *testing.T) {
	s := newTenantSecretsServer(t, newFakeSecretStore())
	// No tenant on the context (resolver never ran / bound nothing).
	req := httptest.NewRequest("GET", "/api/v1/vault/secrets", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for tenant-less request, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "UNAUTHENTICATED")
}

// ─── contract: the operator surface is unchanged ───────────────────────────

// The operator /api/v1/secrets surface stays operator-gated: a caller with
// only a tenant context (no operator session) is rejected by operatorGuard,
// never served. This pins that the tenant surface did not loosen the
// operator one.
func TestOperatorSecretsSurfaceStillGated(t *testing.T) {
	s := newTenantSecretsServer(t, newFakeSecretStore())
	withOperator(s, "") // no operator session
	for _, path := range []string{
		"/api/v1/secrets",
		"/api/v1/secrets/k",
	} {
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, tenantSecretReq("GET", path, tenantA, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: operator surface should reject a non-operator (401), got %d", path, rec.Code)
		}
	}
}
