// SPDX-License-Identifier: Apache-2.0

// shutdown_test.go — Phase 14.3 graceful shutdown coverage.
//
// Verifies:
//
//   - In-flight requests complete successfully after drain starts.
//   - New requests after drain.Start() get 503 + Connection: close.
//   - /ready returns 503 {"status":"draining"} during drain.
//   - /health stays 200 during drain (liveness != readiness).
//   - drain.Start() is idempotent (second call doesn't re-block).
//   - drain.Start() returns DeadlineExceeded when requests outlast
//     the timeout.
//   - The drain counter excludes /health and /ready so probes don't
//     hold the drain window open.
package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

// helper: build a Server with a slow handler registered at /slow that
// blocks until the test releases it. Used to simulate an in-flight
// LLM streaming response during drain.
func newShutdownTestServer(t *testing.T) (*Server, chan struct{}, *sync.WaitGroup) {
	t.Helper()
	cfg := config.Default()
	cfg.Server.ShutdownTimeout = 2 * time.Second
	s := New(cfg, slog.Default(), Deps{})

	release := make(chan struct{})
	wg := &sync.WaitGroup{}
	// Register the slow handler directly on the mux. Because /slow
	// goes through the full middleware chain via httptest, the drain
	// middleware will count it.
	s.mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		wg.Done()
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	return s, release, wg
}

// httpServer fires a request through the full middleware chain. We
// can't use s.mux directly because drain middleware wraps the chain
// outside the mux — we need to hit the same Handler that
// http.Server.Serve would invoke.
func serveHTTP(s *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.srv.Handler.ServeHTTP(rec, req)
	return rec
}

func TestShutdownInFlightRequestCompletes(t *testing.T) {
	s, release, wg := newShutdownTestServer(t)
	wg.Add(1)

	// Kick off the slow request on a goroutine. It'll block in the
	// handler until we close `release`.
	respCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "/slow", nil)
		respCh <- serveHTTP(s, req)
	}()

	// Wait for the handler to actually enter (so the drain counter
	// has been incremented).
	wg.Wait()
	if got := s.drain.Active(); got != 1 {
		t.Fatalf("expected 1 active request, got %d", got)
	}

	// Start drain on a separate goroutine — it should block until
	// the handler releases.
	drainDone := make(chan error, 1)
	go func() {
		drainDone <- s.drain.Start(context.Background())
	}()

	// Give drain a moment to flip the flag.
	time.Sleep(20 * time.Millisecond)
	if !s.drain.IsDraining() {
		t.Fatal("expected IsDraining()=true after Start()")
	}

	// Release the handler.
	close(release)

	// In-flight request should complete with 200.
	select {
	case rec := <-respCh:
		if rec.Code != http.StatusOK {
			t.Errorf("expected in-flight request to complete with 200, got %d", rec.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight request did not complete")
	}

	// Drain should return nil (clean shutdown).
	select {
	case err := <-drainDone:
		if err != nil {
			t.Errorf("expected drain to return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not return after in-flight completed")
	}

	if got := s.drain.Active(); got != 0 {
		t.Errorf("expected 0 active requests after drain, got %d", got)
	}
}

func TestShutdownNewRequestsRejectedDuringDrain(t *testing.T) {
	s, _, _ := newShutdownTestServer(t)

	// Start drain — no in-flight requests so it returns immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := s.drain.Start(ctx); err != nil {
		t.Fatalf("expected clean drain with no in-flight, got %v", err)
	}

	// New request should be rejected with 503.
	req := httptest.NewRequest("POST", "/api/v1/agents/sample.echo",
		strings.NewReader(`{}`))
	rec := serveHTTP(s, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after drain, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Connection") != "close" {
		t.Errorf("expected Connection: close header, got %q", rec.Header().Get("Connection"))
	}
	// Body should be the standard error envelope with code=DRAINING.
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body not JSON: %v", err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj == nil {
		t.Fatal("expected error envelope in response")
	}
	if errObj["code"] != "DRAINING" {
		t.Errorf("expected error.code=DRAINING, got %v", errObj["code"])
	}
}

func TestShutdownReadyReturnsDrainingPayload(t *testing.T) {
	s, _, _ := newShutdownTestServer(t)
	s.MarkReady()

	// Pre-drain: /ready returns 200.
	{
		req := httptest.NewRequest("GET", "/ready", nil)
		rec := serveHTTP(s, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("pre-drain expected 200, got %d", rec.Code)
		}
	}

	// Start drain.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = s.drain.Start(ctx)

	// Post-drain: /ready returns 503 with draining payload + Retry-After.
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := serveHTTP(s, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("post-drain expected 503, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Errorf("expected Retry-After header during drain")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body not JSON: %v", err)
	}
	if body["status"] != "draining" {
		t.Errorf("expected status=draining, got %v", body["status"])
	}
	if _, ok := body["since_s"]; !ok {
		t.Errorf("expected since_s field in draining payload")
	}
}

func TestShutdownHealthStaysAliveDuringDrain(t *testing.T) {
	// Liveness must remain 200 during drain. Kubernetes would kill
	// the pod if liveness 503-ed mid-drain, defeating graceful
	// shutdown.
	s, _, _ := newShutdownTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = s.drain.Start(ctx)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := serveHTTP(s, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected /health to stay 200 during drain, got %d", rec.Code)
	}
}

func TestShutdownProbesSkipDrainCounter(t *testing.T) {
	// /health and /ready must NOT increment the drain counter, or a
	// stuck readiness probe could hold the drain window open forever.
	s, _, _ := newShutdownTestServer(t)
	if got := s.drain.Active(); got != 0 {
		t.Fatalf("expected baseline 0 active, got %d", got)
	}
	for _, path := range []string{"/health", "/ready"} {
		req := httptest.NewRequest("GET", path, nil)
		_ = serveHTTP(s, req)
		if got := s.drain.Active(); got != 0 {
			t.Errorf("after probe %s, expected 0 active, got %d", path, got)
		}
	}
}

func TestShutdownDrainStartIdempotent(t *testing.T) {
	d := NewDrain(500 * time.Millisecond)
	// Second Start should not re-block.
	first := make(chan error, 1)
	go func() { first <- d.Start(context.Background()) }()
	time.Sleep(20 * time.Millisecond)

	second := make(chan error, 1)
	go func() { second <- d.Start(context.Background()) }()

	// Both should complete (no requests in flight).
	for i := 0; i < 2; i++ {
		select {
		case <-first:
		case <-second:
		case <-time.After(1 * time.Second):
			t.Fatalf("idempotent Start did not return (iteration %d)", i)
		}
	}
}

func TestShutdownDrainTimesOutWithInFlight(t *testing.T) {
	s, _, wg := newShutdownTestServer(t)
	wg.Add(1)
	// Fire request that will block past the drain timeout (we never
	// close release).
	go func() {
		req := httptest.NewRequest("GET", "/slow", nil)
		_ = serveHTTP(s, req)
	}()
	wg.Wait()

	// Drain timeout shorter than handler block — expect DeadlineExceeded.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	d := NewDrain(200 * time.Millisecond)
	// Manually bump the counter to simulate the in-flight handler
	// (this test uses its own Drain so we don't fight with the
	// server-owned one).
	d.Inc()
	defer d.Dec()
	err := d.Start(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded with in-flight request, got %v", err)
	}
}

func TestShutdownDrainStartUnblocksOnCounterDrop(t *testing.T) {
	// Pure-controller test (no HTTP layer): verify Start blocks on
	// active>0 and returns nil once active hits 0.
	d := NewDrain(1 * time.Second)
	d.Inc()

	done := make(chan error, 1)
	go func() { done <- d.Start(context.Background()) }()

	// Give Start a moment to enter its poll loop.
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("Start returned before counter dropped: %v", err)
	default:
	}

	d.Dec()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil after counter drop, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Start did not return after counter dropped")
	}
}