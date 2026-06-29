// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/billing"
)

func TestAdapterCreateCustomer(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		email     string
		status    int
		body      interface{}
		wantID    string
		wantError bool
	}{
		{
			name:     "success",
			tenantID: "tenant-1",
			email:    "user@example.com",
			status:   http.StatusOK,
			body: map[string]interface{}{
				"id":         "cus_abc123",
				"tenant_id":  "tenant-1",
				"email":      "user@example.com",
				"plan":       "free",
				"created_at": "2026-06-15T10:00:00Z",
				"updated_at": "2026-06-15T10:00:00Z",
			},
			wantID:    "cus_abc123",
			wantError: false,
		},
		{
			name:     "provider unavailable",
			tenantID: "tenant-1",
			email:    "user@example.com",
			status:   http.StatusServiceUnavailable,
			body: map[string]interface{}{
				"code": "provider_unavailable",
			},
			wantID:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/capabilities" && r.URL.Path != "/v1/customers" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if r.URL.Path == "/v1/capabilities" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"name":              "stripe",
						"version":           "1.0.0",
						"slot":              "billing",
						"protocol_version": "v1",
						"vendor":            "BackAI",
						"capabilities": map[string]interface{}{
							"is_stub": false,
						},
					})
					return
				}
				w.WriteHeader(tt.status)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			adapter, err := New(context.Background(), Config{
				BaseURL: server.URL,
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			id, err := adapter.CreateCustomer(context.Background(), tt.tenantID, tt.email)
			if (err != nil) != tt.wantError {
				t.Errorf("CreateCustomer error = %v, wantError %v", err, tt.wantError)
			}
			if id != tt.wantID {
				t.Errorf("CreateCustomer id = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestAdapterGetCustomer(t *testing.T) {
	tests := []struct {
		name         string
		customerID   string
		status       int
		body         interface{}
		wantEmail    *string
		wantPlan     string
		wantNotFound bool
		wantError    bool
	}{
		{
			name:       "success",
			customerID: "cus_abc123",
			status:     http.StatusOK,
			body: map[string]interface{}{
				"id":         "cus_abc123",
				"tenant_id":  "tenant-1",
				"email":      "user@example.com",
				"plan":       "pro",
				"created_at": "2026-06-15T10:00:00Z",
				"updated_at": "2026-06-15T10:00:00Z",
			},
			wantEmail: stringPtr("user@example.com"),
			wantPlan:  "pro",
			wantError: false,
		},
		{
			name:         "not found",
			customerID:   "cus_notfound",
			status:       http.StatusNotFound,
			body:         map[string]interface{}{"code": "customer_not_found"},
			wantNotFound: true,
			wantError:    true,
		},
		{
			name:       "empty id",
			customerID: "",
			status:     http.StatusOK,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"name":              "stripe",
						"version":           "1.0.0",
						"slot":              "billing",
						"protocol_version": "v1",
						"vendor":            "BackAI",
						"capabilities": map[string]interface{}{
							"is_stub": false,
						},
					})
					return
				}
				w.WriteHeader(tt.status)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			adapter, err := New(context.Background(), Config{
				BaseURL: server.URL,
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			customer, err := adapter.GetCustomer(context.Background(), tt.customerID)
			if tt.wantNotFound {
				if !errors.Is(err, billing.ErrNotFound) {
					t.Errorf("GetCustomer expected ErrNotFound, got %v", err)
				}
				return
			}
			if (err != nil) != tt.wantError {
				t.Errorf("GetCustomer error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil {
				if tt.wantEmail != nil && (customer.Email == nil || *customer.Email != *tt.wantEmail) {
					t.Errorf("GetCustomer email = %v, want %v", customer.Email, tt.wantEmail)
				}
				if customer.Plan != tt.wantPlan {
					t.Errorf("GetCustomer plan = %q, want %q", customer.Plan, tt.wantPlan)
				}
			}
		})
	}
}

func TestAdapterCreatePortalLink(t *testing.T) {
	tests := []struct {
		name         string
		customerID   string
		returnURL    string
		status       int
		body         interface{}
		wantURLMatch bool
		wantError    bool
	}{
		{
			name:       "success with return url",
			customerID: "cus_abc123",
			returnURL:  "https://example.com/billing",
			status:     http.StatusOK,
			body: map[string]interface{}{
				"url":        "https://billing.stripe.com/session/abc123",
				"expires_at": "2026-06-16T10:00:00Z",
			},
			wantURLMatch: true,
			wantError:    false,
		},
		{
			name:         "empty customer id",
			customerID:   "",
			returnURL:    "https://example.com/billing",
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"name":              "stripe",
						"version":           "1.0.0",
						"slot":              "billing",
						"protocol_version": "v1",
						"vendor":            "BackAI",
						"capabilities": map[string]interface{}{
							"is_stub": false,
						},
					})
					return
				}
				w.WriteHeader(tt.status)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			adapter, err := New(context.Background(), Config{
				BaseURL: server.URL,
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			portalLink, err := adapter.CreatePortalLink(context.Background(), tt.customerID, tt.returnURL)
			if (err != nil) != tt.wantError {
				t.Errorf("CreatePortalLink error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil && tt.wantURLMatch {
				if portalLink.URL == "" {
					t.Error("CreatePortalLink URL is empty")
				}
				if portalLink.ExpiresAt == "" {
					t.Error("CreatePortalLink ExpiresAt is empty")
				}
			}
		})
	}
}

func TestAdapterVerifyWebhook(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		status        int
		verified      bool
		wantEventID   string
		wantEventType string
		wantInvalid   bool
		wantError     bool
	}{
		{
			name:          "valid webhook",
			eventType:     "checkout.session.completed",
			status:        http.StatusOK,
			verified:      true,
			wantEventID:   "evt_abc123",
			wantEventType: "checkout.session.completed",
			wantError:     false,
		},
		{
			name:        "invalid signature (response verified=false)",
			eventType:   "charge.updated",
			status:      http.StatusOK,
			verified:    false,
			wantInvalid: true,
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/capabilities" {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]interface{}{
						"name":              "stripe",
						"version":           "1.0.0",
						"slot":              "billing",
						"protocol_version": "v1",
						"vendor":            "BackAI",
						"capabilities": map[string]interface{}{
							"is_stub": false,
						},
					})
					return
				}
				w.WriteHeader(tt.status)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"verified":   tt.verified,
					"event_type": tt.eventType,
					"event_id":   tt.wantEventID,
					"decoded": map[string]interface{}{
						"id":   tt.wantEventID,
						"type": tt.eventType,
					},
				})
			}))
			defer server.Close()

			adapter, err := New(context.Background(), Config{
				BaseURL: server.URL,
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			body := []byte(`{"type": "test"}`)
			sigHeader := "t=1234567890,v1=sig_test"

			event, err := adapter.VerifyWebhook(body, sigHeader)
			if tt.wantInvalid {
				if !errors.Is(err, billing.ErrSignatureInvalid) {
					t.Errorf("VerifyWebhook expected ErrSignatureInvalid, got %v", err)
				}
				return
			}
			if (err != nil) != tt.wantError {
				t.Errorf("VerifyWebhook error = %v, wantError %v", err, tt.wantError)
			}
			if err == nil {
				if event.ID != tt.wantEventID {
					t.Errorf("VerifyWebhook event id = %q, want %q", event.ID, tt.wantEventID)
				}
				// stripe.EventType is a string type, compare as strings
				if string(event.Type) != tt.wantEventType {
					t.Errorf("VerifyWebhook event type = %q, want %q", event.Type, tt.wantEventType)
				}
			}
		})
	}
}

func TestAdapterInterfaceImplementation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":              "stripe",
			"version":           "1.0.0",
			"slot":              "billing",
			"protocol_version": "v1",
			"vendor":            "BackAI",
			"capabilities": map[string]interface{}{
				"is_stub": true,
			},
		})
	}))
	defer server.Close()

	adapter, err := New(context.Background(), Config{
		BaseURL: server.URL,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Verify interface methods exist and work.
	if adapter.AdapterName() != "stripe" {
		t.Errorf("AdapterName = %q, want stripe", adapter.AdapterName())
	}
	if !adapter.IsStub() {
		t.Error("IsStub() should be true")
	}
	if adapter.CachedAt.IsZero() {
		t.Error("CachedAt should be set")
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

// Test base64 encoding of webhook body
func TestWebhookBodyEncoding(t *testing.T) {
	body := []byte(`{"type":"charge.updated"}`)
	encoded := base64.StdEncoding.EncodeToString(body)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(decoded) != string(body) {
		t.Errorf("decoded body = %q, want %q", string(decoded), string(body))
	}
}
