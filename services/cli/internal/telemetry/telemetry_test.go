// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// isolate points HOME at a temp dir and clears telemetry env so each test
// starts from a clean, sink-off baseline.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AF_STACK_TELEMETRY_URL", "")
	t.Setenv("AF_STACK_TELEMETRY", "")
	DefaultURL = "" // ensure no build-time sink leaks into tests
}

// Contract: sink off by default — with no URL configured, telemetry is
// disabled and Emit makes no network call.
func TestNew_NoURL_Disabled(t *testing.T) {
	isolate(t)
	c := New("9.9.9", false, io.Discard)
	if c.enabled {
		t.Fatal("expected telemetry disabled when no URL is set")
	}
	// Emit must be a safe no-op (would panic/hang if it tried to POST nowhere).
	c.Emit(context.Background(), "init", true, 5*time.Millisecond)
}

// Contract: --no-telemetry disables telemetry even when a URL is configured.
func TestNew_OptOutFlag_Disabled(t *testing.T) {
	isolate(t)
	t.Setenv("AF_STACK_TELEMETRY_URL", "http://127.0.0.1:0/collect")
	c := New("9.9.9", true, io.Discard)
	if c.enabled {
		t.Fatal("expected telemetry disabled by --no-telemetry flag")
	}
}

// Contract: AF_STACK_TELEMETRY in {0,false,off,no} disables telemetry.
func TestNew_OptOutEnv_Disabled(t *testing.T) {
	for _, v := range []string{"0", "false", "off", "no", "OFF", "False"} {
		isolate(t)
		t.Setenv("AF_STACK_TELEMETRY_URL", "http://127.0.0.1:0/collect")
		t.Setenv("AF_STACK_TELEMETRY", v)
		if New("9.9.9", false, io.Discard).enabled {
			t.Fatalf("expected telemetry disabled for AF_STACK_TELEMETRY=%q", v)
		}
	}
}

// Contract: with a URL and consent, Emit sends exactly one event carrying only
// non-identifying fields — no arguments, paths, or PII.
func TestEmit_SendsOneAnonymousEvent(t *testing.T) {
	isolate(t)

	var hits int32
	var got Event
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("AF_STACK_TELEMETRY_URL", srv.URL)
	c := New("1.2.3", false, io.Discard)
	if !c.enabled {
		t.Fatal("expected telemetry enabled with URL set + no opt-out")
	}
	c.Emit(context.Background(), "init", true, 250*time.Millisecond)

	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("expected exactly 1 event, got %d", n)
	}
	if got.Schema != schemaID || got.Command != "init" || got.CLIVersion != "1.2.3" {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.AnonID == "" {
		t.Fatal("expected a non-empty anon_id")
	}
	if !got.Success {
		t.Fatal("expected success=true")
	}

	// No PII: the raw payload must not contain anything resembling a path,
	// argument, hostname, or home directory. Assert the JSON keys are exactly
	// the allow-listed set.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawBody), &keys); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	allowed := map[string]bool{
		"schema": true, "command": true, "cli_version": true, "os": true,
		"arch": true, "success": true, "duration_ms": true, "anon_id": true,
		"ts": true,
	}
	for k := range keys {
		if !allowed[k] {
			t.Fatalf("payload contains non-allow-listed field %q (possible PII leak): %s", k, rawBody)
		}
	}
}

// Contract: a stray argument can never ride in via the command field.
func TestSanitizeCommand(t *testing.T) {
	cases := map[string]string{
		"init":                     "init",
		"INIT":                     "init",
		"agent":                    "agent",
		"":                         "unknown",
		"init --name secret":       "unknown",
		"deploy /etc/passwd":       "unknown",
		"mcp call x {\"k\":\"v\"}": "unknown",
	}
	for in, want := range cases {
		if got := sanitizeCommand(in); got != want {
			t.Errorf("sanitizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

// Contract: the per-machine anon id is stable across client constructions.
func TestAnonID_StableAcrossRuns(t *testing.T) {
	isolate(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("AF_STACK_TELEMETRY_URL", srv.URL)

	first := New("1.0.0", false, io.Discard).anonID
	second := New("1.0.0", false, io.Discard).anonID
	if first == "" || first != second {
		t.Fatalf("anon id not stable: %q vs %q", first, second)
	}
}

func TestBucketDuration(t *testing.T) {
	cases := map[time.Duration]int64{
		0:                       0,
		45 * time.Millisecond:   40,
		250 * time.Millisecond:  200,
		1450 * time.Millisecond: 1000,
		3700 * time.Millisecond: 3000,
	}
	for d, want := range cases {
		if got := bucketDuration(d); got != want {
			t.Errorf("bucketDuration(%v) = %d, want %d", d, got, want)
		}
	}
}
