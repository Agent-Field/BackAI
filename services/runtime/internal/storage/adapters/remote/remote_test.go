// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/storage"
)

// TestNew verifies that New() successfully initializes an adapter and fetches capabilities.
func TestNew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":              "mock-storage",
				"version":           "1.0.0",
				"slot":              "storage",
				"protocol_version":  "v1",
				"vendor":            "Test",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes":    int64(5368709120),
					"supports_multipart":       true,
					"presign_ttl_max_seconds":  604800,
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    10 * time.Second,
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if a == nil {
		t.Fatal("adapter is nil")
	}
	if a.Name() != "mock-storage" {
		t.Errorf("expected name 'mock-storage', got %q", a.Name())
	}

	caps := a.Capabilities()
	if caps.MaxObjectSizeBytes != 5368709120 {
		t.Errorf("expected MaxObjectSizeBytes 5368709120, got %d", caps.MaxObjectSizeBytes)
	}
	if !caps.SupportsMultipart {
		t.Errorf("expected SupportsMultipart true, got false")
	}
	if caps.PresignTTLMaxSeconds != 604800 {
		t.Errorf("expected PresignTTLMaxSeconds 604800, got %d", caps.PresignTTLMaxSeconds)
	}
}

// TestNewMissingCapabilities verifies that New() fails when /v1/capabilities is unreachable.
func TestNewMissingCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if a != nil {
		t.Fatal("expected adapter to be nil on error")
	}
	if !strings.Contains(err.Error(), "capabilities probe") {
		t.Errorf("error should mention capabilities probe: %v", err)
	}
}

// TestNewEmptyBaseURL verifies that New() rejects empty BaseURL.
func TestNewEmptyBaseURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL: "",
		Token:   "test-token",
	})

	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}
	if a != nil {
		t.Fatal("expected adapter to be nil")
	}
}

// TestUpload verifies streaming upload to PUT /v1/objects/{key}.
func TestUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects/test-key":
			if r.Method != http.MethodPut {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}

			// Verify streaming body was sent.
			body, _ := io.ReadAll(r.Body)
			if string(body) != "test data" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify Content-Type header is set.
			if r.Header.Get("Content-Type") != "text/plain" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			// Verify X-BackAI-Content-Type mirror is set.
			if r.Header.Get("X-BackAI-Content-Type") != "text/plain" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"key":           "test-key",
				"size":          int64(9),
				"content_type":  "text/plain",
				"etag":          "abc123",
				"last_modified": "2026-06-15T10:00:00Z",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	obj, err := a.Upload(ctx, "test-key", bytes.NewReader([]byte("test data")), storage.UploadOpts{
		ContentType: "text/plain",
		Metadata:    map[string]string{"author": "test"},
	})

	if err != nil {
		t.Fatalf("Upload() failed: %v", err)
	}

	if obj.Key != "test-key" {
		t.Errorf("expected key 'test-key', got %q", obj.Key)
	}
	if obj.Size != 9 {
		t.Errorf("expected size 9, got %d", obj.Size)
	}
	if obj.ContentType != "text/plain" {
		t.Errorf("expected content-type 'text/plain', got %q", obj.ContentType)
	}
	if obj.ETag != "abc123" {
		t.Errorf("expected etag 'abc123', got %q", obj.ETag)
	}
}

// TestDownload verifies streaming download from GET /v1/objects/{key}.
func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects/test-key":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("ETag", `"abc123"`)
			w.Header().Set("Last-Modified", "Mon, 15 Jun 2026 10:00:00 GMT")
			w.Header().Set("Content-Length", "4")
			io.WriteString(w, "data")

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	rc, obj, err := a.Download(ctx, "test-key")
	if err != nil {
		t.Fatalf("Download() failed: %v", err)
	}
	defer rc.Close()

	body, _ := io.ReadAll(rc)
	if string(body) != "data" {
		t.Errorf("expected body 'data', got %q", string(body))
	}

	if obj.ContentType != "image/png" {
		t.Errorf("expected content-type 'image/png', got %q", obj.ContentType)
	}
	if obj.ETag != `"abc123"` {
		t.Errorf("expected etag '\"abc123\"', got %q", obj.ETag)
	}
}

// TestDownloadNotFound verifies that 404 is wrapped with storage.ErrNotFound.
func TestDownloadNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects/missing":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":   "object_not_found",
				"status": 404,
				"title":  "Not Found",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	rc, obj, err := a.Download(ctx, "missing")

	if err != storage.ErrNotFound {
		t.Errorf("expected storage.ErrNotFound, got %v", err)
	}
	if rc != nil {
		t.Errorf("expected rc to be nil on ErrNotFound, got %v", rc)
	}
	if obj != nil {
		t.Errorf("expected obj to be nil on ErrNotFound, got %v", obj)
	}
}

// TestSignedURL verifies POST /v1/objects/{key}/signed-url.
func TestSignedURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes":    int64(1e9),
					"presign_ttl_max_seconds":  604800,
				},
			})

		case "/v1/objects/test-key/signed-url":
			var req signedURLRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.TTLSeconds != 900 {
				t.Errorf("expected TTL 900s, got %d", req.TTLSeconds)
			}
			if req.Method != "GET" {
				t.Errorf("expected method GET, got %s", req.Method)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"url":        "https://example.com/signed?token=xyz",
				"expires_at": "2026-06-15T10:15:00Z",
				"method":     "GET",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	url, err := a.SignedURL(ctx, "test-key", 15*time.Minute)
	if err != nil {
		t.Fatalf("SignedURL() failed: %v", err)
	}

	if url != "https://example.com/signed?token=xyz" {
		t.Errorf("expected specific URL, got %q", url)
	}
}

// TestDelete verifies DELETE /v1/objects/{key}.
func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects/test-key":
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	err = a.Delete(ctx, "test-key")
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
}

// TestDeleteIdempotent verifies that deleting a missing key is not an error.
func TestDeleteIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects/missing":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"code":   "object_not_found",
				"status": 404,
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	err = a.Delete(ctx, "missing")
	if err != nil {
		t.Fatalf("Delete() should be idempotent, got %v", err)
	}
}

// TestList verifies GET /v1/objects?prefix=...&token=...&limit=....
func TestList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/objects":
			prefix := r.URL.Query().Get("prefix")
			if prefix != "tenants/acme/" {
				t.Errorf("expected prefix 'tenants/acme/', got %q", prefix)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"objects": []map[string]interface{}{
					{
						"key":           "tenants/acme/file1.txt",
						"size":          int64(100),
						"content_type":  "text/plain",
						"etag":          "etag1",
						"last_modified": "2026-06-15T10:00:00Z",
					},
					{
						"key":           "tenants/acme/file2.txt",
						"size":          int64(200),
						"content_type":  "text/plain",
						"etag":          "etag2",
						"last_modified": "2026-06-15T11:00:00Z",
					},
				},
				"prefix":     "tenants/acme/",
				"next_token": "",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	result, err := a.List(ctx, "tenants/acme/", "", 1000)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if len(result.Objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(result.Objects))
	}
	if result.Objects[0].Key != "tenants/acme/file1.txt" {
		t.Errorf("expected first key 'tenants/acme/file1.txt', got %q", result.Objects[0].Key)
	}
	if result.NextToken != "" {
		t.Errorf("expected empty next_token, got %q", result.NextToken)
	}
}

// TestEnsureBucket verifies POST /v1/bucket/ensure.
func TestEnsureBucket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})

		case "/v1/bucket/ensure":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ensured": true,
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	err = a.EnsureBucket(ctx)
	if err != nil {
		t.Fatalf("EnsureBucket() failed: %v", err)
	}
}

// TestCapabilities verifies that Capabilities() returns cached values.
func TestCapabilities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/capabilities":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes":    int64(5368709120),
					"supports_multipart":       true,
					"presign_ttl_max_seconds":  604800,
				},
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{BaseURL: srv.URL, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	caps := a.Capabilities()
	if caps.MaxObjectSizeBytes != 5368709120 {
		t.Errorf("expected MaxObjectSizeBytes 5368709120, got %d", caps.MaxObjectSizeBytes)
	}
	if !caps.SupportsMultipart {
		t.Errorf("expected SupportsMultipart true, got false")
	}
}

// TestAuthRequired verifies that Authorization header is sent.
func TestAuthRequired(t *testing.T) {
	authChecked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/capabilities" {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			authChecked = true
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"name":     "mock",
				"slot":     "storage",
				"version":  "1.0.0",
				"capabilities": map[string]interface{}{
					"max_object_size_bytes": int64(1e9),
				},
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		BaseURL:    srv.URL,
		Token:      "my-secret-token",
		Timeout:    10 * time.Second,
		MaxRetries: 0,
	})

	if !authChecked {
		t.Fatal("Authorization header was not checked")
	}
	if a == nil {
		t.Fatalf("adapter should not be nil, got error: %v", err)
	}
}
