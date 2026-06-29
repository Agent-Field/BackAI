// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helper: spin up an httptest.Server with a handler and a Client
// pointed at it, with retry-friendly defaults.
func newTestPair(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{
		BaseURL:    srv.URL,
		Token:      "test-token",
		Timeout:    2 * time.Second,
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func TestClient_GET_Success(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header missing or wrong: %q", got)
		}
		if got := r.Header.Get(HeaderRequestID); got == "" {
			t.Errorf("missing X-Backai-Request-Id")
		}
		if got := r.Header.Get(HeaderRuntimeVersion); got != RuntimeVersion {
			t.Errorf("missing X-Backai-Runtime-Version: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))

	resp, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := resp.DecodeJSON(&out); err != nil {
		t.Fatal(err)
	}
	if out["hello"] != "world" {
		t.Errorf("got %v", out)
	}
}

func TestClient_POST_WriteSendsIdempotencyKey(t *testing.T) {
	var got string
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(HeaderIdempotencyKey)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	resp, err := c.Do(context.Background(), Request{
		Method: http.MethodPost,
		Path:   "/v1/x",
		Body:   map[string]string{"a": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.DecodeJSON(&struct{}{})
	if got == "" {
		t.Errorf("expected idempotency key auto-generated")
	}
}

func TestClient_POST_RespectsCallerIdempotencyKey(t *testing.T) {
	var got string
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(HeaderIdempotencyKey)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	_, err := c.Do(context.Background(), Request{
		Method:         http.MethodPost,
		Path:           "/v1/x",
		Body:           map[string]string{"a": "b"},
		IdempotencyKey: "fixed-key-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "fixed-key-123" {
		t.Errorf("got %q, want fixed-key-123", got)
	}
}

func TestClient_RetryOn5xx(t *testing.T) {
	var attempts atomic.Int32
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, `{"code":"adapter_unavailable","status":503,"title":"Service Unavailable"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	resp, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	var out map[string]bool
	_ = resp.DecodeJSON(&out)
	if !out["ok"] {
		t.Error("payload wrong")
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts (initial + 2 retries), got %d", attempts.Load())
	}
}

func TestClient_NoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintln(w, `{"code":"not_found","status":404}`)
	}))

	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	if !IsCode(err, "not_found") {
		t.Errorf("expected not_found problem, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", attempts.Load())
	}
}

func TestClient_RetryAfterHonored(t *testing.T) {
	start := time.Now()
	var attempts atomic.Int32
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprintln(w, `{"code":"rate_limited","status":429,"retry_after_seconds":1}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("expected ~1s delay from retry_after, took %v", elapsed)
	}
}

func TestClient_TenantIDHeader(t *testing.T) {
	var got string
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(HeaderTenantID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	_, err := c.Do(context.Background(), Request{
		Method:   http.MethodGet,
		Path:     "/v1/x",
		TenantID: "acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "acme" {
		t.Errorf("got %q, want acme", got)
	}
}

func TestClient_ContextCancellationAborts(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := c.Do(ctx, Request{Method: http.MethodGet, Path: "/v1/x"})
	if err == nil {
		t.Fatal("expected error on cancel")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestClient_CapabilitiesCached(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(CapabilityResponse{
			Name:            "test",
			Version:         "1.0.0",
			Slot:            "sandbox",
			ProtocolVersion: "v1",
			Capabilities:    json.RawMessage(`{"supports_gpu":false}`),
		})
	}))

	for i := 0; i < 5; i++ {
		_, err := c.Capabilities(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 capability fetch (cached), got %d", got)
	}
}

func TestClient_StreamingSSE(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprintln(w, `data: {"a":1}`)
		_, _ = fmt.Fprintln(w)
		fl.Flush()
		_, _ = fmt.Fprintln(w, `data: {"a":2}`)
		_, _ = fmt.Fprintln(w)
		fl.Flush()
		_, _ = fmt.Fprintln(w, `event: terminated`)
		_, _ = fmt.Fprintln(w, `data: {"event":"terminated","exit_code":0}`)
		_, _ = fmt.Fprintln(w)
		fl.Flush()
	}))

	resp, err := c.Do(context.Background(), Request{
		Method: http.MethodPost,
		Path:   "/v1/stream",
		Body:   map[string]string{"k": "v"},
		Stream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := []string{}
	for ev := range resp.Events(context.Background()) {
		got = append(got, string(ev.Data))
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[2], "terminated") {
		t.Errorf("expected terminated last, got %v", got[2])
	}
}

func TestClient_StreamingCancellable(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 50; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			_, _ = fmt.Fprintf(w, "data: {\"i\":%d}\n\n", i)
			fl.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   "/v1/stream",
		Body:   map[string]string{},
		Stream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	count := 0
	for ev := range resp.Events(ctx) {
		_ = ev
		count++
		if count == 3 {
			cancel()
		}
	}
	if count < 3 {
		t.Errorf("expected at least 3 events before cancel, got %d", count)
	}
	if count > 20 {
		t.Errorf("expected stream to halt around cancel, got %d events", count)
	}
}

func TestClient_ProblemDecode(t *testing.T) {
	body := `{"type":"https://x","title":"Boom","status":418,"detail":"teapot","code":"teapot","request_id":"abc"}`
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, body)
	}))

	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	p, ok := AsProblem(err)
	if !ok {
		t.Fatalf("expected *Problem, got %T %v", err, err)
	}
	if p.Code != "teapot" || p.HTTPStatus != http.StatusTeapot || p.Detail != "teapot" {
		t.Errorf("decoded wrong: %+v", p)
	}
	if !errors.Is(err, p) {
		t.Errorf("errors.Is should match the Problem")
	}
}

func TestClient_ProblemFallbackWhenBodyMissing(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
	p, ok := AsProblem(err)
	if !ok {
		t.Fatalf("got %T", err)
	}
	if p.Code != "forbidden" || p.HTTPStatus != http.StatusForbidden {
		t.Errorf("expected fallback code from status, got %+v", p)
	}
}

func TestClient_BadURL(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "::not a url"})
	if err == nil {
		t.Error("expected URL parse error")
	}
}

func TestClient_BaseURLRequired(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Error("expected error on empty BaseURL")
	}
}

func TestClient_SchemeRequired(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "/no-scheme"})
	if err == nil {
		t.Error("expected error on missing scheme")
	}
}

func TestClient_QueryParams(t *testing.T) {
	var got string
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	q := make(map[string][]string)
	q["prefix"] = []string{"foo"}
	q["limit"] = []string{"10"}
	_, err := c.Do(context.Background(), Request{
		Method: http.MethodGet,
		Path:   "/v1/x",
		Query:  q,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "prefix=foo") || !strings.Contains(got, "limit=10") {
		t.Errorf("query missing: %s", got)
	}
}

func TestClient_HealthAndInfo(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(HealthResponse{Status: "healthy", UptimeSeconds: 42})
		case "/v1/info":
			_ = json.NewEncoder(w).Encode(InfoResponse{AdminUI: "https://x"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	h, err := c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "healthy" || h.UptimeSeconds != 42 {
		t.Errorf("health: %+v", h)
	}
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.AdminUI != "https://x" {
		t.Errorf("info: %+v", info)
	}
}

func TestClient_InfoMissingIsNotError(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found"}`))
	}))
	info, err := c.Info(context.Background())
	if err != nil {
		t.Errorf("404 on /v1/info should not be an error, got %v", err)
	}
	if info.AdminUI != "" {
		t.Errorf("expected empty info, got %+v", info)
	}
}

func TestClient_CapabilityInvalidate(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(CapabilityResponse{
			Name:            "test",
			Version:         fmt.Sprintf("%d", calls.Load()),
			Slot:            "sandbox",
			ProtocolVersion: "v1",
			Capabilities:    json.RawMessage(`{}`),
		})
	}))

	_, _ = c.Capabilities(context.Background())
	c.caches.invalidate(c.base.String())
	_, _ = c.Capabilities(context.Background())
	if calls.Load() != 2 {
		t.Errorf("expected 2 fetches after invalidate, got %d", calls.Load())
	}
}

func TestClient_ConcurrentDo(t *testing.T) {
	c, _ := newTestPair(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/v1/x"})
			if err != nil {
				t.Errorf("concurrent Do: %v", err)
			}
		}()
	}
	wg.Wait()
}
