package llmcache

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// ─── Construction + argument validation (no DB) ─────────────────────────
//
// These tests run on every `go test ./services/runtime/internal/llmcache/...`
// invocation. The PG-touching tests live in cache_integration_test.go
// behind a build tag so the default suite stays fast and dependency-free.

func TestNewRejectsNilPool(t *testing.T) {
	if _, err := New(nil, nil); err == nil {
		t.Fatal("expected error for nil pool")
	}
}

// Put argument validation runs before any DB call so we can exercise
// the validation surface without a live Postgres.
func TestPutValidatesArguments(t *testing.T) {
	c := &Cache{
		log: slog.Default(),
		now: time.Now,
	}

	cases := []struct {
		name         string
		ttl          time.Duration
		model        string
		response     []byte
		wantContains string
	}{
		{"zero ttl", 0, "m", []byte(`{"x":1}`), "ttl must be positive"},
		{"negative ttl", -time.Second, "m", []byte(`{"x":1}`), "ttl must be positive"},
		{"empty model", time.Minute, "", []byte(`{"x":1}`), "model required"},
		{"empty response", time.Minute, "m", nil, "response must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.Put(context.Background(), "tenant", tc.model, "hash", tc.response, 0, 0, tc.ttl)
			if err == nil {
				t.Fatalf("expected error for case %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantContains)
			}
		})
	}
}

// ─── tenantIDScanner ────────────────────────────────────────────────────

func TestTenantIDScannerHandlesNull(t *testing.T) {
	var dst string
	s := &tenantIDScanner{dst: &dst}
	if err := s.Scan(nil); err != nil {
		t.Fatalf("nil scan: %v", err)
	}
	if dst != "" {
		t.Errorf("expected empty dst on nil, got %q", dst)
	}
}

func TestTenantIDScannerHandlesString(t *testing.T) {
	var dst string
	s := &tenantIDScanner{dst: &dst}
	const uuid = "11111111-1111-1111-1111-111111111111"
	if err := s.Scan(uuid); err != nil {
		t.Fatalf("string scan: %v", err)
	}
	if dst != uuid {
		t.Errorf("expected %q, got %q", uuid, dst)
	}
}

func TestTenantIDScannerHandlesByteArray(t *testing.T) {
	var dst string
	s := &tenantIDScanner{dst: &dst}
	src := [16]byte{
		0x11, 0x11, 0x11, 0x11,
		0x11, 0x11,
		0x11, 0x11,
		0x11, 0x11,
		0x11, 0x11, 0x11, 0x11, 0x11, 0x11,
	}
	if err := s.Scan(src); err != nil {
		t.Fatalf("[16]byte scan: %v", err)
	}
	want := "11111111-1111-1111-1111-111111111111"
	if dst != want {
		t.Errorf("expected %q, got %q", want, dst)
	}
}

func TestTenantIDScannerRejectsUnknownType(t *testing.T) {
	var dst string
	s := &tenantIDScanner{dst: &dst}
	err := s.Scan(42)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// ─── Eviction loop (cancellation; no DB needed) ─────────────────────────

// RunEviction must exit promptly when its context is cancelled. We
// short-circuit Evict by giving the Cache a stubbed pool — easiest way
// to do that without a DB is to drive a parent Cache with nil pool and
// rely on the cancellation path running before any tick.
func TestRunEvictionExitsOnCancel(t *testing.T) {
	c := &Cache{log: slog.Default(), now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Use a long interval so the cancellation path is the only exit.
		c.RunEviction(ctx, time.Hour)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunEviction did not exit after cancel")
	}
}

// ─── ErrNotFound contract ───────────────────────────────────────────────

// Even if callers don't catch ErrNotFound directly (Get returns false on
// miss), the sentinel must remain stable for any future code that does.
func TestErrNotFoundIsSentinel(t *testing.T) {
	if ErrNotFound == nil {
		t.Fatal("ErrNotFound is nil")
	}
	if !errors.Is(ErrNotFound, ErrNotFound) {
		t.Error("ErrNotFound failed self-match")
	}
}
