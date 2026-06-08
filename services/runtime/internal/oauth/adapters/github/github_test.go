// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

func TestExchangeSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]string
		_ = json.Unmarshal(body, &got)
		if got["client_id"] != "cid" || got["code"] != "abc" {
			t.Errorf("unexpected exchange body: %+v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","scope":"repo,read:user","token_type":"bearer"}`))
	}))
	defer ts.Close()

	a := New("cid", "csec", ts.Client())
	orig := tokenURL
	tokenURL = ts.URL
	defer func() { tokenURL = orig }()

	got, err := a.Exchange(context.Background(), "abc", "http://localhost:8080/cb")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if got.AccessToken != "tok" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
	if !got.ExpiresAt.IsZero() {
		t.Fatalf("github tokens should have zero expiry, got %v", got.ExpiresAt)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "repo" || got.Scopes[1] != "read:user" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
}

func TestExchangeErrorBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
	}))
	defer ts.Close()
	a := New("cid", "csec", ts.Client())
	orig := tokenURL
	tokenURL = ts.URL
	defer func() { tokenURL = orig }()
	_, err := a.Exchange(context.Background(), "bad", "http://localhost:8080/cb")
	if !errors.Is(err, oauthtypes.ErrExchangeFailed) {
		t.Fatalf("expected ErrExchangeFailed, got %v", err)
	}
}

func TestRefreshNotSupported(t *testing.T) {
	a := New("cid", "csec", nil)
	_, err := a.Refresh(context.Background(), "rt")
	if !errors.Is(err, oauthtypes.ErrRefreshNotSupported) {
		t.Fatalf("expected ErrRefreshNotSupported, got %v", err)
	}
}

func TestRevokeSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/cid/grant") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	a := New("cid", "csec", ts.Client())
	orig := revokeBase
	revokeBase = ts.URL
	defer func() { revokeBase = orig }()
	if err := a.Revoke(context.Background(), "tok"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
}

func TestAuthorizeURLDefaultScopes(t *testing.T) {
	a := New("cid", "csec", nil)
	got := a.AuthorizeURL("st", nil, "http://localhost:8080/cb")
	for _, want := range []string{"client_id=cid", "state=st", "scope=repo"} {
		if !strings.Contains(got, want) {
			t.Errorf("AuthorizeURL missing %q in %q", want, got)
		}
	}
}
