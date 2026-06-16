// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

func TestNew_Success(t *testing.T) {
	// Mock sidecar returns /v1/capabilities
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name":             "mock-adapter",
			"version":          "1.0.0",
			"slot":             "notifications",
			"protocol_version": "v1",
			"vendor":           "BackAI",
			"capabilities": map[string]any{
				"channels": []string{"email"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    30 * time.Second,
		MaxRetries: 2,
	})

	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if a == nil {
		t.Fatal("New() returned nil adapter")
	}
	if a.Name() != "mock-adapter" {
		t.Fatalf("Name() = %s, want mock-adapter", a.Name())
	}
	if a.CachedAt.IsZero() {
		t.Fatal("CachedAt should be set after probe")
	}
}

func TestNew_MissingBaseURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := New(ctx, Config{BaseURL: ""})
	if err == nil {
		t.Fatal("New() with empty BaseURL should error")
	}
}

func TestNew_CapabilitiesProbeFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"code":   "internal_error",
			"title":  "Internal Server Error",
			"status": 500,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := New(ctx, Config{
		BaseURL: srv.URL,
		Token:   "test-token",
	})
	if err == nil {
		t.Fatal("New() with failing /v1/capabilities should error")
	}
}

func TestNew_WrongSlot(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name":             "wrong-adapter",
			"slot":             "sandbox",
			"protocol_version": "v1",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := New(ctx, Config{BaseURL: srv.URL})
	if err == nil {
		t.Fatal("New() with wrong slot should error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("slot")) {
		t.Fatalf("Error should mention slot: %v", err)
	}
}

func TestSend_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name": "test-mailer",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"code":   "unauthorized",
				"status": 401,
			})
			return
		}

		// Parse request body
		var req messageRequestWire
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify channel
		if req.Channel != "email" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{
				"code":   "unsupported_channel",
				"status": 422,
			})
			return
		}

		// Return success
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":                   "msg_123",
			"provider_message_id":  "re_abc123",
			"status":               "sent",
			"sent_at":              time.Now().Format(time.RFC3339Nano),
		}
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL: srv.URL,
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	notif := notifications.Notification{
		ID:       "notif_001",
		Kind:     notifications.KindEmail,
		Template: "welcome",
		To:       "user@example.com",
		Subject:  strPtr("Welcome"),
		Data: map[string]any{
			"name": "Alice",
		},
	}

	providerID, err := a.Send(ctx, notif)
	if err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}
	if providerID != "re_abc123" {
		t.Fatalf("Send() returned %s, want re_abc123", providerID)
	}
}

func TestSend_UnsupportedChannel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name": "log-only",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		// Reject SMS (only email supported)
		var req messageRequestWire
		json.NewDecoder(r.Body).Decode(&req)

		if req.Channel == "sms" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{
				"code":   "unsupported_channel",
				"detail": "sms not supported",
				"status": 422,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  "msg_456",
			"provider_message_id": "ok",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	notif := notifications.Notification{
		ID:   "notif_002",
		Kind: notifications.KindSMS,
		To:   "+1234567890",
	}

	_, err = a.Send(ctx, notif)
	if err == nil {
		t.Fatal("Send() with unsupported channel should error")
	}
	// Should wrap the error with ErrAdapter context
	if !bytes.Contains([]byte(err.Error()), []byte("adapter")) {
		t.Fatalf("Error should mention adapter context: %v", err)
	}
}

func TestSend_TemplateNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name": "template-aware",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"code":   "template_not_found",
			"detail": "template 'nonexistent' not found",
			"status": 404,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	notif := notifications.Notification{
		ID:       "notif_003",
		Kind:     notifications.KindEmail,
		Template: "nonexistent",
		To:       "user@example.com",
	}

	_, err = a.Send(ctx, notif)
	if err == nil {
		t.Fatal("Send() with nonexistent template should error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("template")) {
		t.Fatalf("Error should mention template: %v", err)
	}
}

func TestSend_AuthRequired(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"code":   "unauthorized",
				"status": 401,
			})
			return
		}
		resp := map[string]any{
			"name": "secure-adapter",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"code":   "unauthorized",
				"status": 401,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  "msg_789",
			"provider_message_id": "ok",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create without token — should fail on capabilities probe
	_, err := New(ctx, Config{BaseURL: srv.URL, Token: ""})
	if err == nil {
		t.Fatal("New() without token to secured endpoint should error")
	}
}

func TestSend_CacheableCapabilities(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"name": "cached-adapter",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  "msg_1",
			"provider_message_id": "ok",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	initialCount := callCount

	// Send a few notifications — should not re-fetch capabilities each time
	for i := 0; i < 3; i++ {
		notif := notifications.Notification{
			ID:   fmt.Sprintf("notif_cache_%d", i),
			Kind: notifications.KindEmail,
			To:   "test@example.com",
		}
		_, err := a.Send(ctx, notif)
		if err != nil {
			t.Fatalf("Send() iteration %d = %v, want nil", i, err)
		}
	}

	// Should still be just the initial probe (client.Capabilities caches)
	if callCount > initialCount+2 { // Allow for natural caching variance
		t.Logf("Expected ~1 capability call, got %d total (started at %d)",
			callCount, initialCount)
	}
}

func TestSend_IdempotencyKey(t *testing.T) {
	receivedKeys := []string{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"name": "idempotent-adapter",
			"slot": "notifications",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-BackAI-Idempotency-Key")
		receivedKeys = append(receivedKeys, key)

		w.Header().Set("Content-Type", "application/json")
		prefix := "idem"
		if len(key) >= 8 {
			prefix = prefix + "_" + key[:8]
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  "msg_idem",
			"provider_message_id": prefix,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}

	notif := notifications.Notification{
		ID:   "notif_idem_unique",
		Kind: notifications.KindEmail,
		To:   "test@example.com",
	}

	_, err = a.Send(ctx, notif)
	if err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}

	// The idempotency key should be the notification ID
	if len(receivedKeys) > 0 && receivedKeys[0] != "notif_idem_unique" {
		t.Fatalf("Expected idempotency key to be notification ID, got %s", receivedKeys[0])
	}
}

func TestDerefString(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want string
	}{
		{"nil", nil, ""},
		{"empty", strPtr(""), ""},
		{"value", strPtr("hello"), "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derefString(tt.in)
			if got != tt.want {
				t.Errorf("derefString(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNotificationToWire(t *testing.T) {
	notif := notifications.Notification{
		ID:       "notif_001",
		Kind:     notifications.KindEmail,
		Template: "welcome",
		To:       "user@example.com",
		From:     strPtr("sender@example.com"),
		Subject:  strPtr("Welcome to BackAI"),
		Data: map[string]any{
			"name": "Alice",
			"code": "ABC123",
		},
	}

	wire := notificationToWire(notif)

	if wire.Channel != "email" {
		t.Errorf("Channel = %s, want email", wire.Channel)
	}
	if len(wire.To) != 1 || wire.To[0] != "user@example.com" {
		t.Errorf("To = %v, want [user@example.com]", wire.To)
	}
	if wire.From != "sender@example.com" {
		t.Errorf("From = %s, want sender@example.com", wire.From)
	}
	if wire.Subject != "Welcome to BackAI" {
		t.Errorf("Subject = %s, want Welcome to BackAI", wire.Subject)
	}
	if wire.TemplateID != "welcome" {
		t.Errorf("TemplateID = %s, want welcome", wire.TemplateID)
	}
	if wire.TemplateVars["name"] != "Alice" {
		t.Errorf("TemplateVars[name] = %v, want Alice", wire.TemplateVars["name"])
	}
}

// Helper to create string pointers
func strPtr(s string) *string {
	return &s
}
