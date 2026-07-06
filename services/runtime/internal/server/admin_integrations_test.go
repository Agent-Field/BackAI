// SPDX-License-Identifier: Apache-2.0

// Package server — admin integrations handler tests.
//
// These exercise the operator-scoped Integrations settings API end to end
// against an in-memory secrets.Store (injected via integrationStoreHook)
// so no live Postgres vault is required. Contract covered:
//   - the surface requires an operator session,
//   - GET reports per-slot field status without leaking raw values,
//   - PUT stores creds under integration/{slot}/{field} and echoes status,
//   - a masked hint never contains the raw secret,
//   - an unknown slot returns a structured 400.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// fakeStore is a minimal in-memory secrets.Store for handler tests. It
// keys by tenantID+"\x00"+key so tenant isolation is honoured.
type fakeStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{data: map[string]string{}} }

func (f *fakeStore) id(tenantID, key string) string { return tenantID + "\x00" + key }

func (f *fakeStore) meta(key string) secrets.SecretMetadata {
	now := time.Now().UTC().Format(time.RFC3339)
	return secrets.SecretMetadata{Key: key, CreatedAt: now, UpdatedAt: now}
}

func (f *fakeStore) Get(_ context.Context, tenantID, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[f.id(tenantID, key)]
	if !ok {
		return nil, secrets.ErrSecretNotFound
	}
	return []byte(v), nil
}

func (f *fakeStore) GetMetadata(_ context.Context, tenantID, key string) (secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[f.id(tenantID, key)]; !ok {
		return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	return f.meta(key), nil
}

func (f *fakeStore) Put(_ context.Context, tenantID, key string, in secrets.PutInput) (secrets.SecretMetadata, error) {
	if key == "" {
		return secrets.SecretMetadata{}, secrets.ErrInvalidKey
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[f.id(tenantID, key)] = in.Value
	return f.meta(key), nil
}

func (f *fakeStore) Delete(_ context.Context, tenantID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.id(tenantID, key)
	if _, ok := f.data[id]; !ok {
		return secrets.ErrSecretNotFound
	}
	delete(f.data, id)
	return nil
}

func (f *fakeStore) List(_ context.Context, tenantID string) ([]secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []secrets.SecretMetadata{}
	prefix := tenantID + "\x00"
	for k := range f.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, f.meta(strings.TrimPrefix(k, prefix)))
		}
	}
	return out, nil
}

func (f *fakeStore) Rotate(_ context.Context, tenantID, key, newValue string) (secrets.SecretMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.id(tenantID, key)
	if _, ok := f.data[id]; !ok {
		return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
	}
	f.data[id] = newValue
	return f.meta(key), nil
}

var _ secrets.Store = (*fakeStore)(nil)

// newIntegrationsTestServer wires a bare server with an operator session and
// an in-memory secrets store, resetting the store hook after the test.
func newIntegrationsTestServer(t *testing.T) (*Server, *fakeStore) {
	t.Helper()
	s := New(config.Default(), slog.Default(), Deps{})
	withOperator(s, "owner")
	store := newFakeStore()
	integrationStoreHook = func(*Server) secrets.Store { return store }
	t.Cleanup(func() { integrationStoreHook = nil })
	return s, store
}

func doJSON(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, r)
	return rec
}

func decodeSlot(t *testing.T, rec *httptest.ResponseRecorder) integrationSlotStatus {
	t.Helper()
	var st integrationSlotStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode slot status: %v / %s", err, rec.Body.String())
	}
	return st
}

// TestAdminIntegrationsRequiresOperator locks the auth contract: neither
// endpoint is reachable without an operator session.
func TestAdminIntegrationsRequiresOperator(t *testing.T) {
	s := New(config.Default(), slog.Default(), Deps{})
	withOperator(s, "") // unauthenticated
	store := newFakeStore()
	integrationStoreHook = func(*Server) secrets.Store { return store }
	t.Cleanup(func() { integrationStoreHook = nil })

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/integrations"},
		{http.MethodPut, "/api/v1/admin/integrations/notifications"},
	} {
		rec := doJSON(t, s, tc.method, tc.path, `{"credentials":{}}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401; body = %s",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestAdminIntegrationsGetReportsUnsetFields verifies GET returns every
// configured slot with all fields present and unset before any writes.
func TestAdminIntegrationsGetReportsUnsetFields(t *testing.T) {
	s, _ := newIntegrationsTestServer(t)

	rec := doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out integrationsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Integrations) != len(integrationFields) {
		t.Fatalf("slots = %d, want %d", len(out.Integrations), len(integrationFields))
	}
	for _, slot := range out.Integrations {
		want := integrationFields[slot.Slot]
		if want == nil {
			t.Fatalf("unexpected slot %q", slot.Slot)
		}
		if len(slot.Fields) != len(want) {
			t.Fatalf("slot %q: fields = %d, want %d", slot.Slot, len(slot.Fields), len(want))
		}
		for _, f := range slot.Fields {
			if f.Set {
				t.Errorf("slot %q field %q reported set before any write", slot.Slot, f.Name)
			}
			if f.Hint != "" {
				t.Errorf("slot %q field %q has a hint before any write", slot.Slot, f.Name)
			}
		}
	}
}

// TestAdminIntegrationsPutStoresAndMasks verifies PUT persists a credential
// under integration/{slot}/{field}, echoes the slot status with the field
// marked set, and that neither the PUT nor GET response leaks the raw value.
func TestAdminIntegrationsPutStoresAndMasks(t *testing.T) {
	s, store := newIntegrationsTestServer(t)

	const rawKey = "re_abc123def456ghi789"
	body := `{"credentials":{"resend_api_key":"` + rawKey + `"}}`
	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/notifications", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Stored under the exact key convention.
	wantKey := "integration/notifications/resend_api_key"
	if got, err := store.Get(context.Background(), s.defaultTenant(nil), wantKey); err != nil {
		t.Fatalf("credential not stored under %q: %v", wantKey, err)
	} else if string(got) != rawKey {
		t.Fatalf("stored value = %q, want %q", got, rawKey)
	}

	// PUT response never contains the raw secret, and the field is set + hinted.
	if strings.Contains(rec.Body.String(), rawKey) {
		t.Fatalf("PUT response leaked raw secret: %s", rec.Body.String())
	}
	st := decodeSlot(t, rec)
	var found *integrationFieldStatus
	for i := range st.Fields {
		if st.Fields[i].Name == "resend_api_key" {
			found = &st.Fields[i]
		}
	}
	if found == nil {
		t.Fatal("resend_api_key missing from PUT response")
	}
	if !found.Set {
		t.Error("resend_api_key not marked set after PUT")
	}
	if found.Hint == "" {
		t.Error("expected a masked hint for a long value")
	}
	if strings.Contains(found.Hint, "abc123def456") {
		t.Errorf("hint leaks secret body: %q", found.Hint)
	}

	// GET agrees and also never leaks the raw value.
	getRec := doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	if strings.Contains(getRec.Body.String(), rawKey) {
		t.Fatalf("GET response leaked raw secret: %s", getRec.Body.String())
	}
}

// TestAdminIntegrationsPutEmptyClears verifies an empty value deletes the
// credential (field flips back to unset).
func TestAdminIntegrationsPutEmptyClears(t *testing.T) {
	s, store := newIntegrationsTestServer(t)
	key := "integration/notifications/slack_webhook_url"
	if _, err := store.Put(context.Background(), s.defaultTenant(nil), key,
		secrets.PutInput{Value: "https://hooks.slack.com/services/XXX/YYY/ZZZ"}); err != nil {
		t.Fatal(err)
	}

	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/notifications",
		`{"credentials":{"slack_webhook_url":""}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get(context.Background(), s.defaultTenant(nil), key); err == nil {
		t.Fatal("expected credential to be cleared")
	}
	st := decodeSlot(t, rec)
	for _, f := range st.Fields {
		if f.Name == "slack_webhook_url" && f.Set {
			t.Error("slack_webhook_url still set after clear")
		}
	}
}

// TestAdminIntegrationsPutUnknownSlot pins the structured 400 for a bad slot.
func TestAdminIntegrationsPutUnknownSlot(t *testing.T) {
	s, _ := newIntegrationsTestServer(t)
	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/bogus",
		`{"credentials":{"x":"y"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "INTEGRATION_SLOT_UNKNOWN")
}

// TestAdminIntegrationsPutUnknownField rejects fields not in the slot map.
func TestAdminIntegrationsPutUnknownField(t *testing.T) {
	s, _ := newIntegrationsTestServer(t)
	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/storage",
		`{"credentials":{"not_a_field":"v"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "INTEGRATION_FIELD_UNKNOWN")
}

// TestAdminIntegrationsNoVaultReturns503 verifies the degrade path.
func TestAdminIntegrationsNoVaultReturns503(t *testing.T) {
	s := New(config.Default(), slog.Default(), Deps{})
	withOperator(s, "owner") // no store hook, no vault
	rec := doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	assertErrorEnvelope(t, rec.Body.Bytes(), "SECRETS_NOT_CONFIGURED")
}
