// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestNative builds a NativeOutbound with no store (we exercise the
// delivery attempt directly) and loopback permitted so httptest servers
// (127.0.0.1) are reachable.
func newTestNative(t *testing.T, secret string) *NativeOutbound {
	t.Helper()
	return NewNativeOutbound(nil, NativeConfig{
		SigningSecret:        secret,
		AllowPrivateNetworks: true,
		Timeout:              5 * time.Second,
	}, nil)
}

// Contract: a 2xx from the subscriber marks the delivery succeeded and the
// request carries a verifiable HMAC signature + event/timestamp headers.
func TestNativeAttemptSuccessSignsBody(t *testing.T) {
	const secret = "whsec_test"
	body := []byte(`{"event":"verdict.ready","case":"A-1"}`)

	var gotSig, gotEvent, gotTS string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get(HeaderSignature)
		gotEvent = r.Header.Get(HeaderEvent)
		gotTS = r.Header.Get(HeaderTimestamp)
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNative(t, secret)
	d := &Delivery{ID: "d1", Destination: srv.URL, EventType: "verdict.ready", Body: body}
	status, code, ms, err := n.attempt(context.Background(), d)

	if err != nil {
		t.Fatalf("attempt err = %v", err)
	}
	if status != StatusSucceeded {
		t.Fatalf("status = %s, want succeeded", status)
	}
	if code != 200 {
		t.Fatalf("code = %d, want 200", code)
	}
	if ms < 0 {
		t.Fatalf("latency = %d, want >= 0", ms)
	}
	if gotEvent != "verdict.ready" {
		t.Errorf("event header = %q", gotEvent)
	}
	if gotTS == "" {
		t.Errorf("timestamp header missing")
	}
	if string(gotBody) != string(body) {
		t.Errorf("body = %q, want %q", gotBody, body)
	}
	// The signature the subscriber receives must verify against the shared
	// secret using the same HMAC scheme the inbound side checks.
	if err := VerifyHMAC(secret, body, gotSig, "sha256"); err != nil {
		t.Errorf("signature did not verify: %v (sig=%q)", err, gotSig)
	}
}

// Contract: with no signing secret the POST still goes out (unsigned) and a
// non-2xx marks the delivery failed with the upstream status recorded.
func TestNativeAttemptNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HeaderSignature) != "" {
			t.Errorf("unexpected signature header when no secret configured")
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := newTestNative(t, "")
	d := &Delivery{ID: "d2", Destination: srv.URL, EventType: "x", Body: []byte(`{}`)}
	status, code, _, err := n.attempt(context.Background(), d)

	if status != StatusFailed {
		t.Fatalf("status = %s, want failed", status)
	}
	if code != 500 {
		t.Fatalf("code = %d, want 500", code)
	}
	if err == nil {
		t.Fatalf("expected an error for 5xx")
	}
}

// Contract: the SSRF guard blocks delivery to a loopback/private
// destination when AllowPrivateNetworks is off — the default production
// posture. The result is a failed attempt, not a panic or a delivery.
func TestNativeAttemptSSRFBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("subscriber must not be reached when SSRF guard is active")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// AllowPrivateNetworks defaults to false here.
	n := NewNativeOutbound(nil, NativeConfig{Timeout: 5 * time.Second}, nil)
	d := &Delivery{ID: "d3", Destination: srv.URL, EventType: "x", Body: []byte(`{}`)}
	status, _, _, err := n.attempt(context.Background(), d)

	if status != StatusFailed {
		t.Fatalf("status = %s, want failed", status)
	}
	if err == nil {
		t.Fatalf("expected an SSRF block error")
	}
}

// Contract: backoff grows exponentially from the base and never exceeds the
// cap.
func TestNativeBackoff(t *testing.T) {
	n := NewNativeOutbound(nil, NativeConfig{
		BackoffBase: time.Second,
		BackoffMax:  10 * time.Second,
	}, nil)
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // capped
		{9, 10 * time.Second}, // still capped
	}
	for _, c := range cases {
		if got := n.backoff(c.attempt); got != c.want {
			t.Errorf("backoff(%d) = %s, want %s", c.attempt, got, c.want)
		}
	}
}

// Contract: without an outbox (no DB) the native service reports
// unconfigured and Enqueue is a clean error, not a panic.
func TestNativeUnconfiguredWithoutDB(t *testing.T) {
	n := NewNativeOutbound(nil, NativeConfig{}, nil)
	if n.Configured() {
		t.Fatalf("Configured() = true without a store")
	}
	if _, err := n.Enqueue(context.Background(), SendInput{URL: "https://x.test", EventType: "e"}); err != ErrNotConfigured {
		t.Fatalf("Enqueue err = %v, want ErrNotConfigured", err)
	}
}
