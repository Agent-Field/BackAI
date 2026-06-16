// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAdminServer returns an httptest server with handlers wired by
// path. Each handler is called with the JSON-decoded body (nil for GET).
// Unhandled paths respond 404.
func fakeAdminServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	return httptest.NewServer(mux)
}

func TestLiteLLMAdmin_Configured(t *testing.T) {
	cases := []struct {
		name      string
		baseURL   string
		masterKey string
		want      bool
	}{
		{"both set", "http://x", "sk-y", true},
		{"no url", "", "sk-y", false},
		{"no key", "http://x", "", false},
		{"nil receiver", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewLiteLLMAdmin(tc.baseURL, tc.masterKey)
			if got := a.Configured(); got != tc.want {
				t.Fatalf("Configured = %v, want %v", got, tc.want)
			}
		})
	}
	// nil receiver
	var nilAdmin *LiteLLMAdmin
	if nilAdmin.Configured() {
		t.Fatalf("nil admin should not be configured")
	}
}

func TestLiteLLMAdmin_GenerateKey_Success(t *testing.T) {
	var received KeyConfig
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/generate": func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer sk-master" {
				t.Errorf("Authorization header = %q, want Bearer sk-master", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"key": "sk-litellm-abc",
				"key_alias": "af-tenant-1-key-1",
				"max_budget": 100.0,
				"rpm_limit": 60,
				"team_id": "tenant-1"
			}`))
		},
	})
	defer server.Close()

	a := NewLiteLLMAdmin(server.URL, "sk-master")
	out, err := a.GenerateKey(context.Background(), KeyConfig{
		Alias:        "af-tenant-1-key-1",
		MaxBudgetUSD: 100.0,
		RPMLimit:     60,
		TenantID:     "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if out.Key != "sk-litellm-abc" {
		t.Errorf("Key = %q, want sk-litellm-abc", out.Key)
	}
	if out.Alias != "af-tenant-1-key-1" {
		t.Errorf("Alias = %q", out.Alias)
	}
	if received.Alias != "af-tenant-1-key-1" {
		t.Errorf("sent Alias = %q", received.Alias)
	}
	if received.MaxBudgetUSD != 100.0 {
		t.Errorf("sent MaxBudgetUSD = %v", received.MaxBudgetUSD)
	}
}

func TestLiteLLMAdmin_GenerateKey_NotConfigured(t *testing.T) {
	a := NewLiteLLMAdmin("", "")
	_, err := a.GenerateKey(context.Background(), KeyConfig{Alias: "x"})
	if !errors.Is(err, ErrLiteLLMNotConfigured) {
		t.Fatalf("err = %v, want ErrLiteLLMNotConfigured", err)
	}
}

func TestLiteLLMAdmin_GenerateKey_RequiresAlias(t *testing.T) {
	a := NewLiteLLMAdmin("http://x", "sk")
	_, err := a.GenerateKey(context.Background(), KeyConfig{})
	if err == nil || !strings.Contains(err.Error(), "alias required") {
		t.Fatalf("err = %v, want alias required", err)
	}
}

func TestLiteLLMAdmin_GenerateKey_UpstreamError(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/generate": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"alias must be unique"}`))
		},
	})
	defer server.Close()

	a := NewLiteLLMAdmin(server.URL, "sk-master")
	_, err := a.GenerateKey(context.Background(), KeyConfig{Alias: "dup"})
	if err == nil {
		t.Fatal("expected error")
	}
	var ae *AdminError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want AdminError", err)
	}
	if ae.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d", ae.StatusCode)
	}
	if !strings.Contains(ae.Body, "alias must be unique") {
		t.Errorf("Body = %q", ae.Body)
	}
}

func TestLiteLLMAdmin_ProbeKeyManagement_StatelessWhenDBMissing(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/info": func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer sk-master" {
				t.Errorf("Authorization header = %q, want Bearer sk-master", got)
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"Database not connected. Connect a database to your proxy"}`))
		},
	})
	defer server.Close()

	a := NewLiteLLMAdmin(server.URL, "sk-master")
	mode, err := a.ProbeKeyManagement(context.Background())
	if err != nil {
		t.Fatalf("ProbeKeyManagement: %v", err)
	}
	if mode != KeyMgmtStateless {
		t.Fatalf("mode = %q, want %q", mode, KeyMgmtStateless)
	}
	if a.VirtualKeysActive() {
		t.Fatalf("VirtualKeysActive = true, want false")
	}
	cached, lastErr := a.KeyManagement()
	if cached != KeyMgmtStateless || lastErr != "" {
		t.Fatalf("cached = %q err=%q, want stateless no err", cached, lastErr)
	}
}

func TestLiteLLMAdmin_ProbeKeyManagement_VirtualKeysWhenKeyInfoReachable(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/info": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"key not found"}`))
		},
	})
	defer server.Close()

	a := NewLiteLLMAdmin(server.URL, "sk-master")
	mode, err := a.ProbeKeyManagement(context.Background())
	if err != nil {
		t.Fatalf("ProbeKeyManagement: %v", err)
	}
	if mode != KeyMgmtVirtualKeys {
		t.Fatalf("mode = %q, want %q", mode, KeyMgmtVirtualKeys)
	}
	if !a.VirtualKeysActive() {
		t.Fatalf("VirtualKeysActive = false, want true")
	}
}

func TestLiteLLMAdmin_ProbeKeyManagement_UnknownOnAuthFailure(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/info": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad master key"}`))
		},
	})
	defer server.Close()

	a := NewLiteLLMAdmin(server.URL, "sk-master")
	mode, err := a.ProbeKeyManagement(context.Background())
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if mode != KeyMgmtUnknown {
		t.Fatalf("mode = %q, want %q", mode, KeyMgmtUnknown)
	}
	if a.VirtualKeysActive() {
		t.Fatalf("VirtualKeysActive = true, want false")
	}
}

func TestLiteLLMAdmin_UpdateKey(t *testing.T) {
	var received KeyConfig
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/update": func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Fatalf("decode: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	if err := a.UpdateKey(context.Background(), "alias-1", KeyConfig{MaxBudgetUSD: 250}); err != nil {
		t.Fatalf("UpdateKey: %v", err)
	}
	if received.Alias != "alias-1" {
		t.Errorf("alias = %q", received.Alias)
	}
	if received.MaxBudgetUSD != 250 {
		t.Errorf("budget = %v", received.MaxBudgetUSD)
	}
}

func TestLiteLLMAdmin_DeleteKey_NewShape(t *testing.T) {
	var seen []byte
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/delete": func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			seen = b
			_, _ = w.Write([]byte(`{"deleted":1}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	if err := a.DeleteKey(context.Background(), "alias-x"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if !strings.Contains(string(seen), `"key_aliases"`) {
		t.Errorf("request body = %s, want key_aliases shape", seen)
	}
}

func TestLiteLLMAdmin_DeleteKey_LegacyFallback(t *testing.T) {
	calls := 0
	var lastBody []byte
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/delete": func(w http.ResponseWriter, r *http.Request) {
			calls++
			b, _ := io.ReadAll(r.Body)
			lastBody = b
			if calls == 1 {
				// First attempt with key_aliases — reject.
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"unknown field key_aliases"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	if err := a.DeleteKey(context.Background(), "alias-y"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(string(lastBody), `"keys"`) {
		t.Errorf("fallback body = %s, want keys shape", lastBody)
	}
}

func TestLiteLLMAdmin_KeyInfo(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/info": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("key_alias") != "alias-1" {
				t.Errorf("alias query = %q", r.URL.Query().Get("key_alias"))
			}
			_, _ = w.Write([]byte(`{
				"key_alias": "alias-1",
				"max_budget": 50,
				"spend": 12.34,
				"rpm_limit": 60
			}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	info, err := a.KeyInfo(context.Background(), "alias-1")
	if err != nil {
		t.Fatalf("KeyInfo: %v", err)
	}
	if info.MaxBudgetUSD == nil || *info.MaxBudgetUSD != 50 {
		t.Errorf("MaxBudgetUSD = %v", info.MaxBudgetUSD)
	}
	if info.CurrentSpendUSD == nil || *info.CurrentSpendUSD != 12.34 {
		t.Errorf("CurrentSpendUSD = %v", info.CurrentSpendUSD)
	}
}

func TestLiteLLMAdmin_KeyInfo_EmptyResponse(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/key/info": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	_, err := a.KeyInfo(context.Background(), "missing")
	if !errors.Is(err, ErrLiteLLMKeyNotFound) {
		t.Fatalf("err = %v, want ErrLiteLLMKeyNotFound", err)
	}
}

func TestLiteLLMAdmin_KeySpend_Flat(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/spend/keys": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"spend": 7.5, "max_budget": 25}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	s, err := a.KeySpend(context.Background(), "alias-1")
	if err != nil {
		t.Fatalf("KeySpend: %v", err)
	}
	if s.TotalUSD != 7.5 {
		t.Errorf("TotalUSD = %v", s.TotalUSD)
	}
	rem, ok := s.RemainingUSD()
	if !ok || rem != 17.5 {
		t.Errorf("RemainingUSD = %v / %v", rem, ok)
	}
}

func TestLiteLLMAdmin_UserSpend_Wrapped(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/spend/users": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{
				"info": [
					{"spend": 1.5, "max_budget": 5},
					{"spend": 2.0, "max_budget": 5}
				]
			}`))
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	s, err := a.UserSpend(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("UserSpend: %v", err)
	}
	if s.TotalUSD != 3.5 {
		t.Errorf("TotalUSD = %v, want 3.5", s.TotalUSD)
	}
	if s.MaxBudgetUSD == nil || *s.MaxBudgetUSD != 10 {
		t.Errorf("MaxBudgetUSD = %v, want 10", s.MaxBudgetUSD)
	}
}

func TestLiteLLMAdmin_KeySpend_NotFound(t *testing.T) {
	server := fakeAdminServer(t, map[string]http.HandlerFunc{
		"/spend/keys": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	defer server.Close()
	a := NewLiteLLMAdmin(server.URL, "sk-master")
	_, err := a.KeySpend(context.Background(), "alias-1")
	if !errors.Is(err, ErrLiteLLMKeyNotFound) {
		t.Fatalf("err = %v, want ErrLiteLLMKeyNotFound", err)
	}
}

func TestSpend_RemainingUSD(t *testing.T) {
	max := 10.0
	s := Spend{TotalUSD: 3, MaxBudgetUSD: &max}
	if r, ok := s.RemainingUSD(); !ok || r != 7 {
		t.Errorf("Remaining = %v / %v", r, ok)
	}
	// Overspend clamps to 0.
	s = Spend{TotalUSD: 50, MaxBudgetUSD: &max}
	if r, ok := s.RemainingUSD(); !ok || r != 0 {
		t.Errorf("Remaining over-budget = %v / %v", r, ok)
	}
	// No budget configured.
	s = Spend{TotalUSD: 50}
	if _, ok := s.RemainingUSD(); ok {
		t.Errorf("expected !ok when MaxBudget nil")
	}
}
