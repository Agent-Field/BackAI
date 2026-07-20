// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

// stripeSig builds a valid Stripe-Signature header for body at time t,
// following Stripe's documented scheme: HMAC-SHA256(secret, "t.body").
func stripeSig(secret string, t time.Time, body []byte) string {
	ts := fmt.Sprintf("%d", t.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return fmt.Sprintf("t=%s,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// githubSig builds a valid X-Hub-Signature-256 header: sha256=HMAC(secret, body).
func githubSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Contract: a correctly-signed Stripe webhook verifies; wrong secret,
// tampered body, and a timestamp outside tolerance all fail.
func TestVerifyStripeSignature(t *testing.T) {
	secret := "whsec_stripeSigningSecret"
	body := []byte(`{"id":"evt_123","type":"charge.succeeded"}`)
	now := time.Unix(1_700_000_000, 0)

	header := stripeSig(secret, now, body)
	if !verifyStripeSignature(secret, header, body, now, stripeDefaultTolerance) {
		t.Fatal("valid signature rejected")
	}

	if verifyStripeSignature("whsec_wrong", header, body, now, stripeDefaultTolerance) {
		t.Fatal("wrong secret accepted")
	}
	if verifyStripeSignature(secret, header, []byte(`{"id":"evt_TAMPERED"}`), now, stripeDefaultTolerance) {
		t.Fatal("tampered body accepted")
	}
	// Verify 10 minutes later — outside the 5-minute tolerance.
	if verifyStripeSignature(secret, header, body, now.Add(10*time.Minute), stripeDefaultTolerance) {
		t.Fatal("stale timestamp accepted")
	}
	// With tolerance disabled, the stale timestamp is fine.
	if !verifyStripeSignature(secret, header, body, now.Add(10*time.Minute), 0) {
		t.Fatal("tolerance=0 should skip the timestamp check")
	}
}

// Contract: Stripe accepts a header carrying multiple v1 signatures as long
// as one matches (key-rotation window).
func TestVerifyStripeMultipleV1(t *testing.T) {
	secret := "whsec_current"
	body := []byte("payload")
	now := time.Unix(1_700_000_000, 0)
	ts := fmt.Sprintf("%d", now.Unix())
	good := stripeSig(secret, now, body) // t=..,v1=<good>
	// Prepend a bogus v1; the good one still matches.
	header := fmt.Sprintf("t=%s,v1=%s,%s", ts, hex.EncodeToString([]byte("deadbeef")), good[len("t=")+len(ts)+1:])
	if !verifyStripeSignature(secret, header, body, now, stripeDefaultTolerance) {
		t.Fatal("expected a matching v1 among several to verify")
	}
}

// Contract: a correctly-signed GitHub webhook verifies; wrong secret and a
// malformed header fail.
func TestVerifyGitHubSignature(t *testing.T) {
	secret := "ghWebhookSecret"
	body := []byte(`{"action":"opened","number":1}`)

	if !verifyGitHubSignature(secret, githubSig(secret, body), body) {
		t.Fatal("valid signature rejected")
	}
	if verifyGitHubSignature("wrong", githubSig(secret, body), body) {
		t.Fatal("wrong secret accepted")
	}
	if verifyGitHubSignature(secret, "sha1=abc", body) {
		t.Fatal("wrong-algorithm header accepted")
	}
	if verifyGitHubSignature(secret, "not-a-signature", body) {
		t.Fatal("malformed header accepted")
	}
	if verifyGitHubSignature("", githubSig(secret, body), body) {
		t.Fatal("empty secret accepted")
	}
}

// Contract: the scheme dispatcher routes to the right algorithm and reports
// ErrWebhookUnsupported for a provider with no signature scheme.
func TestVerifyWebhookSignatureDispatch(t *testing.T) {
	secret := "s3cr3t"
	body := []byte("hello")
	now := time.Unix(1_700_000_000, 0)

	ok, err := VerifyWebhookSignature(WebhookGitHubHMAC, secret, githubSig(secret, body), body, now)
	if err != nil || !ok {
		t.Fatalf("github dispatch: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyWebhookSignature(WebhookStripe, secret, stripeSig(secret, now, body), body, now)
	if err != nil || !ok {
		t.Fatalf("stripe dispatch: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyWebhookSignature(WebhookNone, secret, "", body, now); !errors.Is(err, ErrWebhookUnsupported) {
		t.Fatalf("expected ErrWebhookUnsupported, got %v", err)
	}
}
