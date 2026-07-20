// SPDX-License-Identifier: Apache-2.0

// idempotency_mw_test.go — wired httptest coverage for the PRD R1 inbound
// idempotency middleware. Drives the REAL middleware stack (withLogging →
// tenantResolver → idempotencyMiddleware) against a fake, in-memory store so
// no database is required. The tests are derived from the validation
// contract in idempotency_mw.go, not from the implementation:
//
//   - retry with same key + same request  -> replay, handler runs once
//   - same key + different body           -> 422 IDEMPOTENCY_KEY_REUSED
//   - GET                                 -> never intercepted
//   - no Idempotency-Key                  -> passthrough
//   - key reserved but still in flight    -> 409 IDEMPOTENCY_IN_FLIGHT
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/idempotency"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// fakeIdemStore is an in-memory idempotency.Store keyed by (tenant, key),
// mirroring PgStore's reserve/save/release semantics.
type fakeIdemStore struct {
	mu   sync.Mutex
	rows map[string]*fakeIdemRow
}

type fakeIdemRow struct {
	fingerprint string
	resp        *idempotency.StoredResponse // nil ⇒ still in flight
}

func newFakeIdemStore() *fakeIdemStore {
	return &fakeIdemStore{rows: map[string]*fakeIdemRow{}}
}

func (f *fakeIdemStore) id(ctx context.Context, key string) string {
	return tenantctx.TenantID(ctx) + "|" + key
}

func (f *fakeIdemStore) Reserve(ctx context.Context, key, fingerprint string) (idempotency.Reservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.id(ctx, key)
	row, ok := f.rows[id]
	if !ok {
		f.rows[id] = &fakeIdemRow{fingerprint: fingerprint}
		return idempotency.Reservation{Outcome: idempotency.OutcomeAcquired}, nil
	}
	if row.fingerprint != fingerprint {
		return idempotency.Reservation{Outcome: idempotency.OutcomeMismatch}, nil
	}
	if row.resp == nil {
		return idempotency.Reservation{Outcome: idempotency.OutcomeInFlight}, nil
	}
	return idempotency.Reservation{Outcome: idempotency.OutcomeReplay, Response: row.resp}, nil
}

func (f *fakeIdemStore) Save(ctx context.Context, key string, resp idempotency.StoredResponse) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[f.id(ctx, key)]; ok {
		r := resp
		row.resp = &r
	}
	return nil
}

func (f *fakeIdemStore) Release(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.id(ctx, key)
	if row, ok := f.rows[id]; ok && row.resp == nil {
		delete(f.rows, id)
	}
	return nil
}

// idemHarness wires the real middleware chain around a counting handler.
type idemHarness struct {
	handler http.Handler
	calls   *int64
	store   *fakeIdemStore
}

func newIdemHarness(t *testing.T) idemHarness {
	t.Helper()
	store := newFakeIdemStore()
	srv := New(config.Default(), slog.Default(), Deps{Idempotency: store})
	var calls int64
	counting := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// Body embeds the call count so a replay (which returns the STORED
		// body) is provably identical to the first execution's body.
		_, _ = w.Write([]byte(`{"call":` + strconv.FormatInt(n, 10) + `}`))
	})
	h := srv.idempotencyMiddleware(counting)
	h = srv.tenantResolver(h)
	h = withLogging(srv.log, srv.metricsRing, h)
	return idemHarness{handler: h, calls: &calls, store: store}
}

// envelopeCode extracts error.code from a canonical error envelope body.
func envelopeCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("body not JSON: %v; body=%s", err, body)
	}
	return env.Error.Code
}

func doReq(h http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if key != "" {
		req.Header.Set(idempotencyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestIdempotencyReplaysSameKeyRunsHandlerOnce(t *testing.T) {
	h := newIdemHarness(t)
	const path = "/api/v1/idemtest"

	first := doReq(h.handler, http.MethodPost, path, "k1", `{"a":1}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first call status = %d, want 201; body=%s", first.Code, first.Body.String())
	}
	if got := first.Header().Get(idempotencyReplayedHeader); got != "" {
		t.Errorf("first call should not be a replay, got %s=%q", idempotencyReplayedHeader, got)
	}

	second := doReq(h.handler, http.MethodPost, path, "k1", `{"a":1}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, want 201; body=%s", second.Code, second.Body.String())
	}
	if got := second.Header().Get(idempotencyReplayedHeader); got != "true" {
		t.Errorf("replay missing %s: true header, got %q", idempotencyReplayedHeader, got)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("replay body not byte-identical:\n first=%q\nsecond=%q",
			first.Body.String(), second.Body.String())
	}
	if got := atomic.LoadInt64(h.calls); got != 1 {
		t.Errorf("handler ran %d times, want exactly 1", got)
	}
	if ct := second.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("replay Content-Type = %q, want application/json", ct)
	}
}

func TestIdempotencyKeyReuseDifferentBodyReturns422(t *testing.T) {
	h := newIdemHarness(t)
	const path = "/api/v1/idemtest"

	if rec := doReq(h.handler, http.MethodPost, path, "k2", `{"a":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("first call status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	rec := doReq(h.handler, http.MethodPost, path, "k2", `{"a":2}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("reuse status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec.Body.Bytes(), rec.Code)
	if code := envelopeCode(t, rec.Body.Bytes()); code != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("error.code = %q, want IDEMPOTENCY_KEY_REUSED", code)
	}
	if got := atomic.LoadInt64(h.calls); got != 1 {
		t.Errorf("handler ran %d times, want 1 (second must not execute)", got)
	}
}

func TestIdempotencyGetNotIntercepted(t *testing.T) {
	h := newIdemHarness(t)
	const path = "/api/v1/idemtest"

	first := doReq(h.handler, http.MethodGet, path, "k3", "")
	second := doReq(h.handler, http.MethodGet, path, "k3", "")
	if first.Header().Get(idempotencyReplayedHeader) != "" ||
		second.Header().Get(idempotencyReplayedHeader) != "" {
		t.Errorf("GET must never be replayed")
	}
	if got := atomic.LoadInt64(h.calls); got != 2 {
		t.Errorf("GET handler ran %d times, want 2 (never idempotent)", got)
	}
}

func TestIdempotencyMissingHeaderPassthrough(t *testing.T) {
	h := newIdemHarness(t)
	const path = "/api/v1/idemtest"

	doReq(h.handler, http.MethodPost, path, "", `{"a":1}`)
	doReq(h.handler, http.MethodPost, path, "", `{"a":1}`)
	if got := atomic.LoadInt64(h.calls); got != 2 {
		t.Errorf("no-key handler ran %d times, want 2 (no idempotency)", got)
	}
	if len(h.store.rows) != 0 {
		t.Errorf("no rows should be stored without an Idempotency-Key, got %d", len(h.store.rows))
	}
}

func TestIdempotencyInFlightReturns409(t *testing.T) {
	h := newIdemHarness(t)
	const path = "/api/v1/idemtest"
	const body = `{"a":9}`

	// Pre-seed a reservation with the fingerprint the middleware will
	// compute, but leave it in flight (no saved response).
	fp := idempotency.Fingerprint(http.MethodPost, path, "", []byte(body))
	ctx := tenantctx.WithTenant(context.Background(), "00000000-0000-0000-0000-000000000000", "")
	if _, err := h.store.Reserve(ctx, "k5", fp); err != nil {
		t.Fatalf("seed reserve: %v", err)
	}

	rec := doReq(h.handler, http.MethodPost, path, "k5", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("in-flight status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	assertEnvelope(t, rec.Body.Bytes(), rec.Code)
	if code := envelopeCode(t, rec.Body.Bytes()); code != "IDEMPOTENCY_IN_FLIGHT" {
		t.Errorf("error.code = %q, want IDEMPOTENCY_IN_FLIGHT", code)
	}
	if got := atomic.LoadInt64(h.calls); got != 0 {
		t.Errorf("handler ran %d times, want 0 (in-flight must not execute)", got)
	}
}
