package webhooks

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// hmacHex returns the lowercase hex HMAC of body for the given secret +
// hash function. Used to build the "expected" signature in tests.
func hmacHex(secret string, body []byte, sha string) string {
	var sum []byte
	switch sha {
	case "sha256":
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		sum = mac.Sum(nil)
	case "sha1":
		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write(body)
		sum = mac.Sum(nil)
	default:
		panic("unsupported hash in test helper: " + sha)
	}
	return hex.EncodeToString(sum)
}

func TestVerifyHMACSha256BareHex(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)
	sig := hmacHex(secret, body, "sha256")

	if err := VerifyHMAC(secret, body, sig, "sha256"); err != nil {
		t.Fatalf("VerifyHMAC: %v", err)
	}
}

func TestVerifyHMACSha256GitHubPrefix(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)
	// GitHub sends "sha256=<hex>" — the prefix must be stripped.
	sig := "sha256=" + hmacHex(secret, body, "sha256")

	if err := VerifyHMAC(secret, body, sig, "sha256"); err != nil {
		t.Fatalf("VerifyHMAC with sha256= prefix: %v", err)
	}
}

func TestVerifyHMACSha1(t *testing.T) {
	secret := "topsecret"
	body := []byte("the payload")
	sig := hmacHex(secret, body, "sha1")

	if err := VerifyHMAC(secret, body, sig, "sha1"); err != nil {
		t.Fatalf("VerifyHMAC sha1: %v", err)
	}
}

func TestVerifyHMACMismatchReturnsSentinel(t *testing.T) {
	body := []byte("the payload")
	wrong := hmacHex("other-secret", body, "sha256")

	err := VerifyHMAC("the-real-secret", body, wrong, "sha256")
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got %v", err)
	}
}

func TestVerifyHMACInvalidHexReturnsMismatch(t *testing.T) {
	err := VerifyHMAC("secret", []byte("body"), "not-hex", "sha256")
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch on bad hex, got %v", err)
	}
}

func TestVerifyHMACEmptySignature(t *testing.T) {
	err := VerifyHMAC("secret", []byte("body"), "", "sha256")
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch on empty sig, got %v", err)
	}
}

func TestVerifyHMACUnsupportedAlgo(t *testing.T) {
	err := VerifyHMAC("secret", []byte("body"), "abcdef", "md5")
	if !errors.Is(err, ErrUnsupportedAlgo) {
		t.Fatalf("expected ErrUnsupportedAlgo, got %v", err)
	}
}

func TestVerifyHMACDefaultAlgoIsSha256(t *testing.T) {
	// Empty algorithm string defaults to sha256.
	secret := "topsecret"
	body := []byte("data")
	sig := hmacHex(secret, body, "sha256")

	if err := VerifyHMAC(secret, body, sig, ""); err != nil {
		t.Fatalf("VerifyHMAC with empty algo (should default to sha256): %v", err)
	}
}
