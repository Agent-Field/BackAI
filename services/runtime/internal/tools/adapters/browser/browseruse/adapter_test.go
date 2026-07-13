// SPDX-License-Identifier: Apache-2.0

package browseruse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
)

func TestAdapter_Unconfigured(t *testing.T) {
	a := New("", false)
	if a.Configured() {
		t.Errorf("empty URL: Configured() = true, want false")
	}
	if _, err := a.Navigate(context.Background(), "", "https://x"); !errors.Is(err, browser.ErrNotConfigured) {
		t.Errorf("Navigate unconfigured: err = %v, want ErrNotConfigured", err)
	}
}

func TestAdapter_NavigateRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/navigate" {
			t.Errorf("path = %q, want /navigate", r.URL.Path)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if body["url"] != "https://example.com" {
			t.Errorf("url = %v", body["url"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://example.com","title":"Example","status_code":200}`))
	}))
	defer srv.Close()

	// httptest listens on 127.0.0.1, which safehttp blocks by default —
	// allowPrivate=true is exactly the escape hatch for that.
	a := New(srv.URL, true)
	res, err := a.Navigate(context.Background(), "session-1", "https://example.com")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if res.Title != "Example" || res.StatusCode != 200 {
		t.Errorf("res = %+v, want title Example / status 200", res)
	}
}

func TestAdapter_PrivateBlockedByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// Without allowPrivate the loopback sidecar must be SSRF-blocked.
	a := New(srv.URL, false)
	if _, err := a.Navigate(context.Background(), "", "https://example.com"); err == nil {
		t.Fatal("Navigate to loopback sidecar succeeded, want safehttp block")
	} else if !strings.Contains(err.Error(), "block") {
		t.Logf("err = %v (expected a safehttp block; message wording may vary)", err)
	}
}

func TestAdapter_ID(t *testing.T) {
	a := New("http://example.com", false)
	if a.ID() != "browser-use" {
		t.Errorf("ID = %q, want browser-use", a.ID())
	}
}
