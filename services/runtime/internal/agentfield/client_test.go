package agentfield

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected /health, got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok", Version: "1.0.0"})
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, RequestTimeout: time.Second})
	hs, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if hs.Status != "ok" {
		t.Errorf("expected status=ok, got %q", hs.Status)
	}
}

func TestHealthFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error on 500")
	}
}

func TestHealthFailsOnUnreachable(t *testing.T) {
	c := New(Config{URL: "http://127.0.0.1:1", RequestTimeout: 200 * time.Millisecond})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error when AF unreachable")
	}
}

func TestDiscoverReturnsEmptyOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL})
	agents, err := c.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected empty, got %v", agents)
	}
}

func TestNewURLNormalisation(t *testing.T) {
	c := New(Config{URL: "http://example.com/"})
	if c.BaseURL() != "http://example.com" {
		t.Errorf("expected trailing slash trimmed, got %q", c.BaseURL())
	}
}

func TestEmptyURLRejected(t *testing.T) {
	c := New(Config{})
	if _, err := c.Health(context.Background()); err == nil {
		t.Error("expected error for empty URL")
	}
	if _, err := c.Discover(context.Background()); err == nil {
		t.Error("expected error for empty URL")
	}
}
