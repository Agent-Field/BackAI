// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
)

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
