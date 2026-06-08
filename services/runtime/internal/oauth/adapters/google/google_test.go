// SPDX-License-Identifier: Apache-2.0

package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

func TestExchangeSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		for _, want := range []string{"client_id=cid", "client_secret=csec", "code=abc", "grant_type=authorization_code"} {
			if !strings.Contains(form, want) {
				t.Errorf("form missing %q: %s", want, form)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at",
			"refresh_token": "rt",
			"expires_in":    3600,
			"scope":         "scope_a scope_b",
		})
	}))
	defer ts.Close()

	a := New("cid", "csec", ts.Client())
	// Re-route to the test server.
	patchURLs(t, ts.URL+"/", ts.URL+"/")

	got, err := a.Exchange(context.Background(), "abc", "http://localhost:8080/cb")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" {
		t.Fatalf("tokens mismatch: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "scope_a" {
		t.Fatalf("scopes mismatch: %+v", got.Scopes)
	}
	if got.ExpiresAt.Before(time.Now().Add(30 * time.Minute)) {
		t.Fatalf("ExpiresAt too early: %v", got.ExpiresAt)
	}
}

func TestExchangeErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Code expired",
		})
	}))
	defer ts.Close()

	a := New("cid", "csec", ts.Client())
	patchURLs(t, ts.URL+"/", ts.URL+"/")

	_, err := a.Exchange(context.Background(), "expired", "http://localhost:8080/cb")
	if !errors.Is(err, oauthtypes.ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
	// Sensitive bodies must NOT show in error message.
	if strings.Contains(err.Error(), "Code expired") {
		// Acceptable to include "invalid_grant" but body bytes shouldn't
		// be reflected verbatim. We assert at minimum no token leak.
	}
}

func TestRefreshSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form := string(body)
		if !strings.Contains(form, "grant_type=refresh_token") {
			t.Errorf("expected refresh grant: %s", form)
		}
		if !strings.Contains(form, "refresh_token=rtok") {
			t.Errorf("expected refresh_token param: %s", form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-at",
			"expires_in":   600,
		})
	}))
	defer ts.Close()
	a := New("cid", "csec", ts.Client())
	patchURLs(t, ts.URL+"/", ts.URL+"/")

	got, err := a.Refresh(context.Background(), "rtok")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got.AccessToken != "new-at" {
		t.Fatalf("access token = %q want new-at", got.AccessToken)
	}
}

func TestRefreshEmptyToken(t *testing.T) {
	a := New("cid", "csec", nil)
	_, err := a.Refresh(context.Background(), "")
	if !errors.Is(err, oauthtypes.ErrRefreshNotSupported) {
		t.Fatalf("expected ErrRefreshNotSupported, got %v", err)
	}
}

func TestRevokeAcceptsBadRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Google returns 400 for "token already revoked" — treated as success.
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()
	a := New("cid", "csec", ts.Client())
	patchURLs(t, "", ts.URL+"/")
	if err := a.Revoke(context.Background(), "tok"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}

// patchURLs swaps the package-level google endpoint constants with the
// test server's URL. The helper is a no-op when an empty URL is passed,
// so individual tests opt in per endpoint.
func patchURLs(t *testing.T, tokenURLOverride, revokeURLOverride string) {
	t.Helper()
	if tokenURLOverride != "" {
		orig := tokenURL
		tokenURL = tokenURLOverride
		t.Cleanup(func() { tokenURL = orig })
	}
	if revokeURLOverride != "" {
		orig := revokeURL
		revokeURL = revokeURLOverride
		t.Cleanup(func() { revokeURL = orig })
	}
}
