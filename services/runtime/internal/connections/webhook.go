// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// stripeDefaultTolerance is the max age of a Stripe webhook timestamp we
// accept, matching Stripe's own SDK default. Guards against replay of an
// old-but-validly-signed payload.
const stripeDefaultTolerance = 5 * time.Minute

// VerifyWebhookSignature dispatches to the provider's signature scheme.
// secret is the connection's webhook signing secret; header is the raw
// provider signature header value; body is the exact raw request body.
// Returns (false, ErrWebhookUnsupported) when the provider declares no
// scheme. now is injected for deterministic Stripe timestamp checks.
func VerifyWebhookSignature(scheme WebhookScheme, secret, header string, body []byte, now time.Time) (bool, error) {
	switch scheme {
	case WebhookStripe:
		return verifyStripeSignature(secret, header, body, now, stripeDefaultTolerance), nil
	case WebhookGitHubHMAC:
		return verifyGitHubSignature(secret, header, body), nil
	case WebhookNone:
		return false, ErrWebhookUnsupported
	default:
		return false, ErrWebhookUnsupported
	}
}

// verifyStripeSignature validates a `Stripe-Signature` header of the form
// `t=<unix>,v1=<hexhmac>[,v1=<hexhmac>...]`. The signed payload is
// "<t>.<body>" and the MAC is HMAC-SHA256(secret, signedPayload). Any v1
// entry matching (constant-time) passes. When tolerance > 0 the timestamp
// must be within tolerance of now (replay guard).
func verifyStripeSignature(secret, header string, body []byte, now time.Time, tolerance time.Duration) bool {
	if secret == "" || header == "" {
		return false
	}
	var ts string
	var v1s []string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			v1s = append(v1s, kv[1])
		}
	}
	if ts == "" || len(v1s) == 0 {
		return false
	}
	if tolerance > 0 {
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return false
		}
		delta := now.Sub(time.Unix(sec, 0))
		if delta < 0 {
			delta = -delta
		}
		if delta > tolerance {
			return false
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)
	for _, v1 := range v1s {
		got, err := hex.DecodeString(v1)
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) {
			return true
		}
	}
	return false
}

// verifyGitHubSignature validates an `X-Hub-Signature-256` header of the
// form `sha256=<hexhmac>` where the MAC is HMAC-SHA256(secret, body).
func verifyGitHubSignature(secret, header string, body []byte) bool {
	if secret == "" || header == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(got, expected)
}
