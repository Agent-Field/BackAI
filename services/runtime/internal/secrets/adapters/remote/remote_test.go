// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

type mock struct {
	caps     map[string]any
	store    map[string]storedSecret
	getCalls int
}

type storedSecret struct {
	value       string
	description string
	createdAt   string
	updatedAt   string
}

func newMock() (*mock, *httptest.Server) {
	m := &mock{
		caps: map[string]any{
			"name":             "test-secrets",
			"version":          "1.0.0",
			"slot":             "secrets",
			"protocol_version": "v1",
			"capabilities": map[string]any{
				"supports_versioning": true,
				"supports_rotation":   true,
				"supports_reveal":     true,
				"kms_backend":         "test",
				"max_value_bytes":     65536,
				"audit_log_revealed":  true,
			},
		},
		store: make(map[string]storedSecret),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(m.caps)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})
	mux.HandleFunc("/v1/secrets", func(w http.ResponseWriter, _ *http.Request) {
		items := make([]map[string]any, 0, len(m.store))
		for k, s := range m.store {
			items = append(items, map[string]any{
				"key":        k,
				"version":    1,
				"created_at": s.createdAt,
				"updated_at": s.updatedAt,
				"metadata":   map[string]string{"description": s.description},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": items})
	})
	mux.HandleFunc("/v1/secrets/", func(w http.ResponseWriter, r *http.Request) {
		// /v1/secrets/{key} | /v1/secrets/{key}/reveal | /v1/secrets/{key}/rotate
		path := strings.TrimPrefix(r.URL.Path, "/v1/secrets/")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		parts := strings.SplitN(path, "/", 2)
		key := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}
		switch {
		case action == "reveal" && r.Method == http.MethodPost:
			m.getCalls++
			s, ok := m.store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"secret_not_found","status":404}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": key, "version": 1, "value": s.value, "revealed_at": "now",
			})
		case action == "rotate" && r.Method == http.MethodPost:
			s, ok := m.store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"secret_not_found","status":404}`))
				return
			}
			var body struct {
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.value = body.Value
			s.updatedAt = "rotated-at"
			m.store[key] = s
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": key, "version": 2, "last_rotated_at": "rotated-at",
			})
		case action == "" && r.Method == http.MethodGet:
			s, ok := m.store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"secret_not_found","status":404}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key":        key,
				"version":    1,
				"created_at": s.createdAt,
				"updated_at": s.updatedAt,
				"metadata":   map[string]string{"description": s.description},
			})
		case action == "" && r.Method == http.MethodPut:
			var body struct {
				Value    string            `json:"value"`
				Metadata map[string]string `json:"metadata"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.store[key] = storedSecret{
				value:       body.Value,
				description: body.Metadata["description"],
				createdAt:   "created-at",
				updatedAt:   "updated-at",
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": key, "version": 1, "created_at": "created-at",
				"updated_at": "updated-at",
				"metadata":   body.Metadata,
			})
		case action == "" && r.Method == http.MethodDelete:
			if _, ok := m.store[key]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"secret_not_found","status":404}`))
				return
			}
			delete(m.store, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "{}", http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	return m, srv
}

func newAdapter(t *testing.T) (*mock, *Adapter) {
	t.Helper()
	m, srv := newMock()
	t.Cleanup(srv.Close)
	a, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return m, a
}

func TestAdapter_PutGet(t *testing.T) {
	_, a := newAdapter(t)
	_, err := a.Put(context.Background(), "acme", "openai_api_key", secrets.PutInput{Value: "sk-abc", Description: "openai"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Get(context.Background(), "acme", "openai_api_key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sk-abc" {
		t.Errorf("got %q", got)
	}
}

func TestAdapter_GetMissing(t *testing.T) {
	_, a := newAdapter(t)
	_, err := a.Get(context.Background(), "acme", "nope")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestAdapter_GetMetadataMissing(t *testing.T) {
	_, a := newAdapter(t)
	_, err := a.GetMetadata(context.Background(), "acme", "nope")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestAdapter_PutGetMetadata(t *testing.T) {
	_, a := newAdapter(t)
	_, err := a.Put(context.Background(), "acme", "k1", secrets.PutInput{Value: "v", Description: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	md, err := a.GetMetadata(context.Background(), "acme", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if md.Key != "k1" || md.Description == nil || *md.Description != "desc" {
		t.Errorf("got %+v", md)
	}
}

func TestAdapter_Delete(t *testing.T) {
	_, a := newAdapter(t)
	_, _ = a.Put(context.Background(), "acme", "k1", secrets.PutInput{Value: "v"})
	if err := a.Delete(context.Background(), "acme", "k1"); err != nil {
		t.Fatal(err)
	}
	_, err := a.Get(context.Background(), "acme", "k1")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestAdapter_DeleteMissing(t *testing.T) {
	_, a := newAdapter(t)
	err := a.Delete(context.Background(), "acme", "nope")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestAdapter_List(t *testing.T) {
	_, a := newAdapter(t)
	_, _ = a.Put(context.Background(), "acme", "k1", secrets.PutInput{Value: "v1", Description: "d1"})
	_, _ = a.Put(context.Background(), "acme", "k2", secrets.PutInput{Value: "v2"})
	items, err := a.List(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("list size: %d", len(items))
	}
}

func TestAdapter_Rotate(t *testing.T) {
	_, a := newAdapter(t)
	_, _ = a.Put(context.Background(), "acme", "k1", secrets.PutInput{Value: "v1"})
	_, err := a.Rotate(context.Background(), "acme", "k1", "v2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Get(context.Background(), "acme", "k1")
	if err != nil || string(got) != "v2" {
		t.Errorf("rotate failed: %s err=%v", got, err)
	}
}

func TestAdapter_RotateMissing(t *testing.T) {
	_, a := newAdapter(t)
	_, err := a.Rotate(context.Background(), "acme", "nope", "v")
	if !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Errorf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestAdapter_RejectsWrongSlot(t *testing.T) {
	m, srv := newMock()
	defer srv.Close()
	m.caps["slot"] = "billing"
	_, err := New(context.Background(), Config{BaseURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "slot") {
		t.Errorf("expected slot mismatch, got %v", err)
	}
}

func TestAdapter_CapabilitiesParsed(t *testing.T) {
	_, a := newAdapter(t)
	if !a.Caps.SupportsRotation || !a.Caps.SupportsReveal {
		t.Errorf("caps not parsed: %+v", a.Caps)
	}
	if a.Caps.MaxValueBytes != 65536 {
		t.Errorf("max_value_bytes: %d", a.Caps.MaxValueBytes)
	}
}

func TestAdapter_TenantPassthrough(t *testing.T) {
	_, srv := newMock()
	defer srv.Close()
	// Override to inspect tenant header.
	var seenTenant string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":             "x",
			"version":          "1",
			"slot":             "secrets",
			"protocol_version": "v1",
			"capabilities":     map[string]any{},
		})
	})
	mux.HandleFunc("/v1/secrets/k1", func(w http.ResponseWriter, r *http.Request) {
		seenTenant = r.Header.Get("X-Backai-Tenant-Id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": "k1", "version": 1, "created_at": "x", "updated_at": "x",
		})
	})
	srv2 := httptest.NewServer(mux)
	defer srv2.Close()

	a, err := New(context.Background(), Config{BaseURL: srv2.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = a.GetMetadata(context.Background(), "acme", "k1")
	if seenTenant != "acme" {
		t.Errorf("tenant header: %q", seenTenant)
	}
}

func TestAdapter_InterfaceSatisfaction(t *testing.T) {
	// Compile-time check duplicated as a runtime no-op to guarantee
	// the file is built.
	var _ secrets.Store = (*Adapter)(nil)
}
