// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/auth"
)

func TestVerifySessionHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{"supports_mfa": true},
			})
			return
		}
		if r.URL.Path == "/v1/sessions/verify" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"user_id":      "usr_abc123",
				"email":        "alice@example.com",
				"tenant_id":    "ten_xyz789",
				"roles":        []string{"admin"},
				"expires_at":   "2026-06-15T12:00:00Z",
				"mfa_verified": true,
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	identity, err := adapter.VerifySession(context.Background(), "token123")
	if err != nil {
		t.Fatalf("VerifySession failed: %v", err)
	}
	if identity.UserID != "usr_abc123" {
		t.Errorf("expected user_id=usr_abc123, got %s", identity.UserID)
	}
	if identity.Email != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %s", identity.Email)
	}
	if identity.TenantID != "ten_xyz789" {
		t.Errorf("expected tenant_id=ten_xyz789, got %s", identity.TenantID)
	}
	if len(identity.Roles) != 1 || identity.Roles[0] != "admin" {
		t.Errorf("expected roles=[admin], got %v", identity.Roles)
	}
	if !identity.MFAVerified {
		t.Error("expected MFAVerified=true")
	}
}

func TestVerifySessionInvalidToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/sessions/verify" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"type":       "https://docs.backai.dev/errors/auth/invalid-token",
				"title":      "Invalid Token",
				"status":     401,
				"code":       "invalid_token",
				"detail":     "Token signature verification failed.",
				"request_id": "req123",
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = adapter.VerifySession(context.Background(), "invalid_token")
	if err != auth.ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken, got %v", err)
	}
}

func TestVerifySessionExpiredToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/sessions/verify" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"type":       "https://docs.backai.dev/errors/auth/expired-token",
				"title":      "Token Expired",
				"status":     401,
				"code":       "expired_token",
				"detail":     "Token is past its expiration time.",
				"request_id": "req123",
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = adapter.VerifySession(context.Background(), "expired_token")
	if err != auth.ErrExpiredToken {
		t.Errorf("expected ErrExpiredToken, got %v", err)
	}
}

func TestGetUserHappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/users/usr_abc123" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":           "usr_abc123",
				"email":        "alice@example.com",
				"name":         "Alice Smith",
				"created_at":   "2026-01-15T10:00:00Z",
				"mfa_enrolled": true,
				"providers":    []string{"google", "github"},
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	user, err := adapter.GetUser(context.Background(), "usr_abc123")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.ID != "usr_abc123" {
		t.Errorf("expected id=usr_abc123, got %s", user.ID)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %s", user.Email)
	}
	if user.Name != "Alice Smith" {
		t.Errorf("expected name=Alice Smith, got %s", user.Name)
	}
	if !user.MFAEnrolled {
		t.Error("expected MFAEnrolled=true")
	}
	if len(user.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(user.Providers))
	}
}

func TestGetUserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/users/missing_user" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{
				"type":       "https://docs.backai.dev/errors/auth/user-not-found",
				"title":      "User Not Found",
				"status":     404,
				"code":       "user_not_found",
				"detail":     "User does not exist.",
				"request_id": "req123",
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = adapter.GetUser(context.Background(), "missing_user")
	if err != auth.ErrUserNotFound {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestCapabilityCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities": map[string]any{
					"supports_mfa":             true,
					"session_lifetime_seconds": 3600,
				},
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 capability fetch during New, got %d", callCount)
	}

	// Call Capabilities() multiple times
	for i := 0; i < 3; i++ {
		caps := adapter.Capabilities()
		if !caps.SupportsMFA {
			t.Error("expected SupportsMFA=true")
		}
		if caps.SessionLifetimeSeconds != 3600 {
			t.Errorf("expected session_lifetime_seconds=3600, got %d", caps.SessionLifetimeSeconds)
		}
	}

	// Should not have triggered additional fetches
	if callCount != 1 {
		t.Errorf("expected cached capabilities (still 1 fetch), got %d fetches", callCount)
	}
}

func TestSlotMismatchRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "wrong-slot",
				"version":          "1.0.0",
				"slot":             "sandbox",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
		}
	}))
	defer server.Close()

	_, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err == nil {
		t.Fatal("expected error on slot mismatch")
	}
	if fmt.Sprintf("%v", err) != "auth/remote: capability probe: auth/remote: adapter reports slot=\"sandbox\"; expected auth" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAuthHeaderSent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			// Check auth header on capabilities fetch
			if auth := r.Header.Get("Authorization"); auth != "Bearer test_token" {
				t.Errorf("expected Authorization header, got %q", auth)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/sessions/verify" {
			// Check auth header on verify call
			if auth := r.Header.Get("Authorization"); auth != "Bearer test_token" {
				t.Errorf("expected Authorization header on verify, got %q", auth)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"user_id":      "usr_test",
				"email":        "test@example.com",
				"tenant_id":    "ten_test",
				"expires_at":   "2026-06-15T12:00:00Z",
				"mfa_verified": false,
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Token:      "test_token",
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	_, err = adapter.VerifySession(context.Background(), "token123")
	if err != nil {
		t.Fatalf("VerifySession failed: %v", err)
	}
}

func TestNameMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "better-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	name := adapter.Name()
	if name != "better-auth" {
		t.Errorf("expected name=better-auth, got %s", name)
	}
}

func TestEmptyRolesAndProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"name":             "test-auth",
				"version":          "1.0.0",
				"slot":             "auth",
				"protocol_version": "v1",
				"vendor":           "test",
				"capabilities":     map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/sessions/verify" {
			w.Header().Set("Content-Type", "application/json")
			// Send null roles to test nil handling
			json.NewEncoder(w).Encode(map[string]any{
				"user_id":      "usr_abc",
				"email":        "test@example.com",
				"tenant_id":    "ten_xyz",
				"roles":        nil,
				"expires_at":   "2026-06-15T12:00:00Z",
				"mfa_verified": false,
			})
			return
		}
		if r.URL.Path == "/v1/users/usr_abc" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":           "usr_abc",
				"email":        "test@example.com",
				"name":         "Test User",
				"created_at":   "2026-01-15T10:00:00Z",
				"mfa_enrolled": false,
				"providers":    nil,
			})
		}
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	identity, err := adapter.VerifySession(context.Background(), "token")
	if err != nil {
		t.Fatalf("VerifySession failed: %v", err)
	}
	if identity.Roles == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(identity.Roles) != 0 {
		t.Errorf("expected empty roles, got %v", identity.Roles)
	}

	user, err := adapter.GetUser(context.Background(), "usr_abc")
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if user.Providers == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(user.Providers) != 0 {
		t.Errorf("expected empty providers, got %v", user.Providers)
	}
}

func TestNilAdapter(t *testing.T) {
	var adapter *Adapter
	caps := adapter.Capabilities()
	if caps.SessionLifetimeSeconds != 0 {
		t.Errorf("expected zero capabilities on nil adapter")
	}
	name := adapter.Name()
	if name != "" {
		t.Errorf("expected empty name on nil adapter, got %s", name)
	}
}
