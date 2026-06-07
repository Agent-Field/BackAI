// verify.go — HMAC signature verification for inbound webhooks.
//
// Different providers spell their signature header differently. We
// support the two most common shapes:
//
//   - sha256 — GitHub-style: header value is "sha256=<hex>". The
//     prefix is stripped before comparison; "sha256=" is optional so
//     callers that omit it (custom providers) still work.
//   - sha1   — older providers (GitHub legacy, some webhook proxies)
//     emit "sha1=<hex>"; the same prefix-strip rule applies.
//
// Comparison is constant-time (hmac.Equal) to avoid leaking the
// expected signature byte-by-byte through timing.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
)

// VerifyHMAC checks `signature` against HMAC(algorithm, secret, body).
//
// Returns nil on a successful match. Returns ErrSignatureMismatch on
// any failure path (wrong hex, wrong length, mismatched MAC) — callers
// must not differentiate. Returns ErrUnsupportedAlgo when algorithm is
// neither "sha256" nor "sha1".
//
// The expected signature accepts both bare hex and prefixed form
// ("<algo>=<hex>"); GitHub uses the prefix, Stripe / Shopify don't.
func VerifyHMAC(secret string, body []byte, signature, algorithm string) error {
	algorithm = strings.ToLower(strings.TrimSpace(algorithm))
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return ErrSignatureMismatch
	}
	// Strip "<algo>=" prefix if present. We accept either the matching
	// algo prefix or the wrong one (some providers send the algo as a
	// prefix even when it doesn't match the chosen algorithm config).
	if i := strings.IndexByte(signature, '='); i >= 0 {
		signature = signature[i+1:]
	}

	var newHash func() hash.Hash
	switch algorithm {
	case "sha256", "":
		newHash = sha256.New
	case "sha1":
		newHash = sha1.New
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAlgo, algorithm)
	}

	mac := hmac.New(newHash, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	provided, err := hex.DecodeString(signature)
	if err != nil {
		return ErrSignatureMismatch
	}
	if !hmac.Equal(expected, provided) {
		return ErrSignatureMismatch
	}
	return nil
}
