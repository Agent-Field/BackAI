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
	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
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

// TestAdminIntegrationsOAuthSlotRoundTrip verifies each OAuth provider is
// registered as an integration slot (oauth_<provider>) with client_id +
// client_secret, that a PUT persists both under the integration/{slot}/{field}
// convention, and that the response is masked and lists the slot on GET.
func TestAdminIntegrationsOAuthSlotRoundTrip(t *testing.T) {
	s, store := newIntegrationsTestServer(t)

	// Registry parity: every oauth.AllProviderNames provider has a slot.
	for _, provider := range oauth.AllProviderNames {
		slot := "oauth_" + provider
		fields, ok := integrationFields[slot]
		if !ok {
			t.Fatalf("missing integration slot for oauth provider %q", provider)
		}
		if len(fields) != 2 || fields[0] != "client_id" || fields[1] != "client_secret" {
			t.Fatalf("slot %q fields = %v, want [client_id client_secret]", slot, fields)
		}
	}

	const cid, csec = "cid-123456789", "csec-abcdefghij"
	body := `{"credentials":{"client_id":"` + cid + `","client_secret":"` + csec + `"}}`
	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/oauth_google", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Stored under integration/oauth_google/<field>.
	for field, want := range map[string]string{"client_id": cid, "client_secret": csec} {
		key := "integration/oauth_google/" + field
		got, err := store.Get(context.Background(), s.defaultTenant(nil), key)
		if err != nil {
			t.Fatalf("%s not stored: %v", key, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	// Response is masked and both fields are marked set.
	if strings.Contains(rec.Body.String(), csec) {
		t.Fatalf("PUT leaked raw client secret: %s", rec.Body.String())
	}
	st := decodeSlot(t, rec)
	if st.Slot != "oauth_google" || len(st.Fields) != 2 {
		t.Fatalf("unexpected oauth_google status: %+v", st)
	}
	for _, f := range st.Fields {
		if !f.Set {
			t.Errorf("field %q not set after PUT", f.Name)
		}
	}

	// GET lists oauth_google alongside the adapter slots.
	getRec := doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	if strings.Contains(getRec.Body.String(), csec) {
		t.Fatalf("GET leaked raw client secret: %s", getRec.Body.String())
	}
	var out integrationsListResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, slot := range out.Integrations {
		if slot.Slot == "oauth_google" {
			found = true
		}
	}
	if !found {
		t.Fatal("oauth_google slot missing from GET response")
	}
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

// TestAdminIntegrationsSandboxBrowserSlots pins the two capability slots the
// dashboard uses for sandbox / browser credential entry: both must exist,
// accept a round-trip, and mark non-secret fields with kind "text" so the
// UI can render them unmasked.
func TestAdminIntegrationsSandboxBrowserSlots(t *testing.T) {
	s, _ := newIntegrationsTestServer(t)

	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/sandbox",
		`{"credentials":{"e2b_api_key":"e2b_test_1234567890"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT sandbox status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/browser",
		`{"credentials":{"browser_use_url":"http://browser-sidecar:8000","allow_private":"true"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT browser status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	var out integrationsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byName := map[string]integrationSlotStatus{}
	for _, sl := range out.Integrations {
		byName[sl.Slot] = sl
	}
	sb, ok := byName["sandbox"]
	if !ok {
		t.Fatal("sandbox slot missing from GET")
	}
	br, ok := byName["browser"]
	if !ok {
		t.Fatal("browser slot missing from GET")
	}

	fields := func(sl integrationSlotStatus) map[string]integrationFieldStatus {
		m := map[string]integrationFieldStatus{}
		for _, f := range sl.Fields {
			m[f.Name] = f
		}
		return m
	}
	sbf, brf := fields(sb), fields(br)

	if !sbf["e2b_api_key"].Set {
		t.Error("sandbox e2b_api_key should be set after PUT")
	}
	if sbf["e2b_api_key"].Kind != "" {
		t.Errorf("e2b_api_key is a secret, kind = %q", sbf["e2b_api_key"].Kind)
	}
	if !brf["browser_use_url"].Set || !brf["allow_private"].Set {
		t.Error("browser fields should be set after PUT")
	}
	for _, name := range []string{"browser_use_url", "playwright_endpoint", "allow_private", "browserbase_project_id"} {
		if brf[name].Kind != "text" {
			t.Errorf("browser field %q kind = %q, want text", name, brf[name].Kind)
		}
	}
	if brf["steel_api_key"].Kind != "" || brf["browserbase_api_key"].Kind != "" {
		t.Error("provider API keys must stay secret (empty kind)")
	}
	if sbf["e2b_base_url"].Default != "https://api.e2b.app" {
		t.Errorf("e2b_base_url should advertise its default, got %q", sbf["e2b_base_url"].Default)
	}
	if brf["allow_private"].Default != "false" {
		t.Errorf("allow_private should advertise its default, got %q", brf["allow_private"].Default)
	}
	if sbf["e2b_api_key"].Default != "" {
		t.Error("e2b_api_key has no default and must not advertise one")
	}
	if brf["browserbase_project_id"].Note == "" {
		t.Error("browserbase_project_id is optional (inferred from key) and must carry a note")
	}
	if sbf["remote_token"].Note == "" {
		t.Error("remote_token is optional and must carry a note")
	}
	if brf["steel_api_key"].Note != "" || brf["steel_api_key"].Default != "" {
		t.Error("steel_api_key is required — no note/default")
	}
}

// TestAdminIntegrationsProvidersConsistent pins the provider-grouping
// contract: every provider field must exist in the slot's flat field
// list, every multi-provider slot's fields must all be reachable through
// some provider, and single-provider slots get the implicit group.
func TestAdminIntegrationsProvidersConsistent(t *testing.T) {
	for slot, provs := range integrationProviders {
		flat := map[string]bool{}
		for _, f := range integrationFields[slot] {
			flat[f] = true
		}
		if len(flat) == 0 {
			t.Errorf("integrationProviders has slot %q with no integrationFields entry", slot)
			continue
		}
		covered := map[string]bool{}
		for _, p := range provs {
			if p.ID == "" || p.Label == "" {
				t.Errorf("slot %q provider %+v missing id or label", slot, p)
			}
			for _, f := range p.Fields {
				if !flat[f] {
					t.Errorf("slot %q provider %q names unknown field %q", slot, p.ID, f)
				}
				covered[f] = true
			}
		}
		for f := range flat {
			if !covered[f] {
				t.Errorf("slot %q field %q not reachable through any provider", slot, f)
			}
		}
	}

	s, _ := newIntegrationsTestServer(t)
	rec := doJSON(t, s, http.MethodGet, "/api/v1/admin/integrations", "")
	var out integrationsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, sl := range out.Integrations {
		if len(sl.Providers) == 0 {
			t.Errorf("slot %q returned no providers", sl.Slot)
		}
		if len(integrationProviders[sl.Slot]) == 0 {
			if len(sl.Providers) != 1 || len(sl.Providers[0].Fields) != len(sl.Fields) {
				t.Errorf("slot %q implicit provider should carry all %d fields, got %+v", sl.Slot, len(sl.Fields), sl.Providers)
			}
		}
	}
}
