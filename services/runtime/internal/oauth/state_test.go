// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := "test-secret-please-rotate"
	state := State{
		TenantID: "00000000-0000-0000-0000-000000000000",
		UserID:   "11111111-1111-1111-1111-111111111111",
		Provider: "google",
		ReturnTo: "http://localhost:3001/integrations",
		IssuedAt: time.Now().Unix(),
		Nonce:    "abc123",
	}
	signed, err := SignState(secret, state)
	if err != nil {
		t.Fatalf("SignState: %v", err)
	}
	if !strings.Contains(signed, ".") {
		t.Fatalf("signed token missing dot separator: %q", signed)
	}
	got, err := VerifyState(secret, signed)
	if err != nil {
		t.Fatalf("VerifyState: %v", err)
	}
	if got.TenantID != state.TenantID || got.UserID != state.UserID ||
		got.Provider != state.Provider || got.ReturnTo != state.ReturnTo ||
		got.Nonce != state.Nonce {
		t.Fatalf("state mismatch after round-trip: got %+v want %+v", got, state)
	}
}

func TestVerifyStateTampered(t *testing.T) {
	secret := "test-secret"
	signed, err := SignState(secret, State{
		TenantID: "t", UserID: "u", Provider: "google",
		IssuedAt: time.Now().Unix(), Nonce: "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte in the payload portion.
	parts := strings.SplitN(signed, ".", 2)
	tampered := parts[0][:len(parts[0])-1] + "X" + "." + parts[1]
	if _, err := VerifyState(secret, tampered); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for tampered payload, got %v", err)
	}

	// Flip a byte in the signature. Pick a guaranteed-different hex char
	// so this test cannot accidentally leave the signature unchanged.
	replacement := "0"
	if strings.HasSuffix(parts[1], "0") {
		replacement = "1"
	}
	tampered = parts[0] + "." + parts[1][:len(parts[1])-1] + replacement
	if _, err := VerifyState(secret, tampered); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for tampered signature, got %v", err)
	}
}

func TestVerifyStateWrongSecret(t *testing.T) {
	signed, err := SignState("secret-a", State{
		TenantID: "t", UserID: "u", Provider: "google",
		IssuedAt: time.Now().Unix(), Nonce: "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyState("secret-b", signed); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for wrong secret, got %v", err)
	}
}

func TestVerifyStateExpired(t *testing.T) {
	signed, err := SignState("secret", State{
		TenantID: "t", UserID: "u", Provider: "google",
		IssuedAt: time.Now().Add(-1 * time.Hour).Unix(),
		Nonce:    "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyState("secret", signed); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for expired state, got %v", err)
	}
}

func TestVerifyStateFromFuture(t *testing.T) {
	signed, err := SignState("secret", State{
		TenantID: "t", UserID: "u", Provider: "google",
		IssuedAt: time.Now().Add(10 * time.Minute).Unix(),
		Nonce:    "n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyState("secret", signed); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for future state, got %v", err)
	}
}

func TestVerifyStateEmptySecret(t *testing.T) {
	if _, err := VerifyState("", "anything"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState for empty secret, got %v", err)
	}
	if _, err := SignState("", State{TenantID: "t"}); err == nil {
		t.Fatalf("expected error signing with empty secret")
	}
}

func TestValidateReturnTo(t *testing.T) {
	allow := []string{"http://localhost:3001", "https://app.example.com"}
	cases := []struct {
		name  string
		input string
		ok    bool
	}{
		{"empty", "", true},
		{"allowed_localhost", "http://localhost:3001/integrations?x=1", true},
		{"allowed_https", "https://app.example.com/oauth/done", true},
		{"disallowed_other_host", "https://evil.com/x", false},
		{"disallowed_scheme", "javascript:alert(1)", false},
		{"disallowed_data", "data:text/html,x", false},
		{"disallowed_file", "file:///etc/passwd", false},
		{"malformed", "://nope", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateReturnTo(c.input, allow)
			if c.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !c.ok && !errors.Is(err, ErrInvalidReturnTo) {
				t.Fatalf("expected ErrInvalidReturnTo, got %v", err)
			}
		})
	}

	// Empty allow list: any http(s) URL is accepted.
	if err := ValidateReturnTo("https://example.com/x", nil); err != nil {
		t.Fatalf("expected pass with empty allow list, got %v", err)
	}
	if err := ValidateReturnTo("javascript:alert(1)", nil); !errors.Is(err, ErrInvalidReturnTo) {
		t.Fatalf("expected ErrInvalidReturnTo for javascript: with nil allow list, got %v", err)
	}
}

func TestRandomNonce(t *testing.T) {
	a := RandomNonce()
	b := RandomNonce()
	if a == b {
		t.Fatalf("two consecutive nonces should differ: %q == %q", a, b)
	}
	if len(a) != 32 { // 16 bytes hex
		t.Fatalf("nonce length = %d, want 32", len(a))
	}
}
