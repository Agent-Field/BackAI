// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"bytes"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// fakeKeyCipher builds a deterministic AES-256 cipher from a fixed 32-byte
// key so credential sealing is testable without any KMS/env setup.
func fakeKeyCipher(t *testing.T) Cipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := secrets.NewCipherFromKey(key)
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	return c
}

// Contract: a credential sealed with the shared secrets cipher round-trips
// back to the identical plaintext, and the ciphertext contains no plaintext
// token bytes.
func TestSealCredentialRoundTrip(t *testing.T) {
	c := fakeKeyCipher(t)
	cred := credential{
		AccessToken:  "gho_liveAccessTokenABC123",
		RefreshToken: "ghr_refreshTokenXYZ789",
	}

	sealed, err := sealCredential(c, cred)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("gho_liveAccessTokenABC123")) ||
		bytes.Contains(sealed, []byte("ghr_refreshTokenXYZ789")) {
		t.Fatal("ciphertext leaked plaintext token material")
	}

	got, err := openCredential(c, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != cred {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, cred)
	}
}

// Contract: an api_key credential round-trips too, and the token() accessor
// returns the right field per kind.
func TestSealAPIKeyRoundTripAndTokenAccessor(t *testing.T) {
	c := fakeKeyCipher(t)
	cred := credential{APIKey: "sk_test_secretKeyValue"}

	sealed, err := sealCredential(c, cred)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := openCredential(c, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got != cred {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, cred)
	}
	if got.token(KindAPIKey) != "sk_test_secretKeyValue" {
		t.Fatalf("token(api_key) = %q", got.token(KindAPIKey))
	}
	if got.token(KindOAuth) != "" {
		t.Fatalf("token(oauth) should be empty for api_key cred, got %q", got.token(KindOAuth))
	}
}

// Contract: an empty envelope decrypts to the zero credential (an OAuth
// connection may legitimately have no stored secret before consent).
func TestOpenEmptyEnvelope(t *testing.T) {
	c := fakeKeyCipher(t)
	got, err := openCredential(c, nil)
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	if got != (credential{}) {
		t.Fatalf("expected zero credential, got %+v", got)
	}
}

// Contract: ciphertext sealed under one key does not open under a different
// key (the GCM auth tag fails) — proving the envelope is genuinely keyed.
func TestOpenWithWrongKeyFails(t *testing.T) {
	c1 := fakeKeyCipher(t)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(255 - i)
	}
	c2, err := secrets.NewCipherFromKey(key2)
	if err != nil {
		t.Fatalf("build cipher2: %v", err)
	}

	sealed, err := sealCredential(c1, credential{APIKey: "sk_live_value"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := openCredential(c2, sealed); err == nil {
		t.Fatal("expected decrypt under wrong key to fail")
	}
}
