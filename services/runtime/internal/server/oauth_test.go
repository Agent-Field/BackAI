// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// getOAuthProviders drives GET /api/v1/oauth/providers and decodes it.
func getOAuthProviders(t *testing.T, s *Server) []oauth.ProviderInfo {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/oauth/providers", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("providers status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []oauth.ProviderInfo `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	return body.Providers
}

func findOAuthProvider(list []oauth.ProviderInfo, name string) oauth.ProviderInfo {
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	return oauth.ProviderInfo{}
}

// TestOAuthProvidersReportRedirectURI verifies the providers response
// carries a per-provider redirect_uri derived from AF_STACK_PUBLIC_URL.
func TestOAuthProvidersReportRedirectURI(t *testing.T) {
	for _, n := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv("OAUTH_"+n+"_CLIENT_ID", "")
		t.Setenv("OAUTH_"+n+"_CLIENT_SECRET", "")
	}
	t.Setenv("AF_STACK_PUBLIC_URL", "https://api.example.test")
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()

	for _, p := range getOAuthProviders(t, s) {
		want := "https://api.example.test/oauth/callback/" + p.Name
		if p.RedirectURI != want {
			t.Fatalf("provider %q redirect_uri = %q, want %q", p.Name, p.RedirectURI, want)
		}
		// Unconfigured providers report an empty credentials_source.
		if p.CredentialsSource != "" {
			t.Fatalf("provider %q source = %q, want empty", p.Name, p.CredentialsSource)
		}
	}
}

// TestOAuthProvidersEnvSource verifies env-provided creds surface as
// credentials_source "env".
func TestOAuthProvidersEnvSource(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "env-id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "env-secret")
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "")
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()

	g := findOAuthProvider(getOAuthProviders(t, s), "google")
	if !g.Configured || g.CredentialsSource != "env" {
		t.Fatalf("expected env-configured google, got %+v", g)
	}
}

// TestOAuthVaultCredsTakeEffectWithoutRestart is the end-to-end no-restart
// contract: with no env creds, saving client_id+client_secret via the admin
// integrations API flips the provider to configured (source "vault") on the
// next providers read AND lets authorize build a consent URL — all on the
// same running Server.
func TestOAuthVaultCredsTakeEffectWithoutRestart(t *testing.T) {
	for _, n := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv("OAUTH_"+n+"_CLIENT_ID", "")
		t.Setenv("OAUTH_"+n+"_CLIENT_SECRET", "")
	}
	t.Setenv("AF_STACK_AUTH_SECRET", "test-secret")
	t.Setenv("AF_STACK_PUBLIC_URL", "https://api.example.test")

	store := newFakeStore()
	integrationStoreHook = func(*Server) secrets.Store { return store }
	t.Cleanup(func() { integrationStoreHook = nil })

	// New() wires the vault-first resolver onto the factory. The resolver
	// reads integrationStore() lazily, so the hook above is honoured.
	s := New(config.Default(), slog.Default(), Deps{OAuthFactory: oauth.NewFactoryFromEnv()})
	withOperator(s, "owner")
	s.oauthManager = &oauth.Manager{} // sentinel; authorize never reaches Token/Store

	// Before any creds: google unconfigured, empty source, but redirect_uri present.
	g := findOAuthProvider(getOAuthProviders(t, s), "google")
	if g.Configured || g.CredentialsSource != "" {
		t.Fatalf("google should start unconfigured, got %+v", g)
	}
	if g.RedirectURI != "https://api.example.test/oauth/callback/google" {
		t.Fatalf("redirect_uri = %q", g.RedirectURI)
	}

	// Operator saves creds via the admin API — no restart, same *Server.
	putBody := `{"credentials":{"client_id":"vault-cid","client_secret":"vault-csec"}}`
	rec := doJSON(t, s, http.MethodPut, "/api/v1/admin/integrations/oauth_google", putBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Next providers read reflects vault creds.
	g = findOAuthProvider(getOAuthProviders(t, s), "google")
	if !g.Configured || g.CredentialsSource != "vault" {
		t.Fatalf("expected configured google via vault, got %+v", g)
	}

	// authorize now succeeds and the consent URL carries the vault client id.
	areq := httptest.NewRequest("POST", "/api/v1/oauth/google/authorize", strings.NewReader(`{}`))
	areq = areq.WithContext(tenantctx.WithTenantAndUser(areq.Context(), "tenant-1", "key-1", "user-1"))
	arec := httptest.NewRecorder()
	s.mux.ServeHTTP(arec, areq)
	if arec.Code != http.StatusOK {
		t.Fatalf("authorize status = %d body=%s", arec.Code, arec.Body.String())
	}
	var ares struct {
		AuthorizationURL string `json:"authorization_url"`
		RedirectURI      string `json:"redirect_uri"`
	}
	if err := json.Unmarshal(arec.Body.Bytes(), &ares); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ares.AuthorizationURL, "client_id=vault-cid") {
		t.Fatalf("consent URL missing vault client id: %q", ares.AuthorizationURL)
	}
	if ares.RedirectURI != "https://api.example.test/oauth/callback/google" {
		t.Fatalf("authorize redirect_uri = %q", ares.RedirectURI)
	}

	// The raw secret must never appear in the authorize response.
	if strings.Contains(arec.Body.String(), "vault-csec") {
		t.Fatalf("authorize response leaked client secret: %s", arec.Body.String())
	}
}

func TestOAuthProvidersListsAllWhenFactoryUnset(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/oauth/providers", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []oauth.ProviderInfo `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 0 {
		t.Fatalf("expected empty providers list without factory, got %d", len(body.Providers))
	}
}

func TestOAuthProvidersListsAllWhenFactorySet(t *testing.T) {
	// Clear all env so nothing is configured.
	for _, n := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv("OAUTH_"+n+"_CLIENT_ID", "")
		t.Setenv("OAUTH_"+n+"_CLIENT_SECRET", "")
	}
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()
	req := httptest.NewRequest("GET", "/api/v1/oauth/providers", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Providers []oauth.ProviderInfo `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != len(oauth.AllProviderNames) {
		t.Fatalf("expected %d providers, got %d", len(oauth.AllProviderNames), len(body.Providers))
	}
	for _, p := range body.Providers {
		if p.Configured {
			t.Errorf("provider %q reports configured with no env keys", p.Name)
		}
	}
}

func TestOAuthConnectionsReturns503WithoutManager(t *testing.T) {
	s := newBareTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/oauth/connections", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OAUTH_NOT_CONFIGURED") {
		t.Errorf("expected OAUTH_NOT_CONFIGURED in body: %s", rec.Body.String())
	}
}

func TestOAuthTokenRequiresInternalHeader(t *testing.T) {
	// Provider configured + factory; manager nil so we hit unavailable
	// first. Re-test with a faked manager-like presence by using both
	// factory and an arbitrary non-nil manager pointer.
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "secret")
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()
	// Without a manager the route returns 503; we want to hit the
	// internal-only guard FIRST, so wire a manager too. The Manager
	// constructor needs a pool, which we don't have, so create a stub.
	// Since handleOAuthToken checks oauthManager != nil before the
	// header, we set it to a sentinel pointer.
	s.oauthManager = &oauth.Manager{} // zero-value Manager; OK because the test never reaches Token().

	req := httptest.NewRequest("POST", "/api/v1/oauth/token",
		strings.NewReader(`{"provider":"google"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without internal header, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INTERNAL_ONLY") {
		t.Errorf("expected INTERNAL_ONLY error code in body: %s", rec.Body.String())
	}
}

func TestOAuthCallbackRejectsInvalidState(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("AF_STACK_AUTH_SECRET", "test-secret")
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()
	s.oauthManager = &oauth.Manager{}

	req := httptest.NewRequest("GET", "/oauth/callback/google?code=abc&state=tampered", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OAUTH_INVALID_STATE") {
		t.Errorf("expected OAUTH_INVALID_STATE, body=%s", rec.Body.String())
	}
}

func TestOAuthCallbackRejectsProviderDenied(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "secret")
	s := newBareTestServer(t)
	s.oauthFactory = oauth.NewFactoryFromEnv()
	s.oauthManager = &oauth.Manager{}

	req := httptest.NewRequest("GET", "/oauth/callback/google?error=access_denied", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on provider denial, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OAUTH_PROVIDER_DENIED") {
		t.Errorf("expected OAUTH_PROVIDER_DENIED, body=%s", rec.Body.String())
	}
}
