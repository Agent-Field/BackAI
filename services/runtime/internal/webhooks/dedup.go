// SPDX-License-Identifier: Apache-2.0

// dedup.go — deterministic dedup token derivation for inbound webhooks.
//
// The inbound handler writes (endpoint_id, dedup_token) into the
// suite_webhook_deliveries partial-unique index. The token must be:
//
//   1. Deterministic: the same provider POST must produce the same
//      token on retry, so a duplicate write fails the index.
//   2. Cheap: derived from the request alone (headers + body), no
//      extra DB lookup.
//
// We pick a provider-specific header where one exists (GitHub puts a
// per-delivery UUID in X-GitHub-Delivery; Stripe embeds a timestamp
// + signature pair in Stripe-Signature that's unique per attempt
// modulo retries); otherwise we fall back to a sha256 of the body so
// idempotent retransmits of the same payload collide on the index.
package webhooks

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Dedup returns a stable dedup token for the request. The endpointID is
// not hashed in — the index already keys by (endpoint_id, dedup_token)
// so two different endpoints receiving the same payload don't collide.
//
// Providers we know about override the body-hash fallback:
//
//	github  -> X-GitHub-Delivery (a uuid per delivery attempt)
//	stripe  -> the "t=..." timestamp + first 16 hex of "v1=..."
//	          from Stripe-Signature (unique per attempt; retries reuse
//	          the same value)
//	shopify -> X-Shopify-Webhook-Id
//
// For any other provider we hash the raw body. Two POSTs with
// identical bodies (e.g. an over-eager provider redelivering on a
// network blip) will collide on the index and the second insert will
// fail with ErrDuplicateDelivery — exactly what the inbound handler
// wants.
//
// The token is bounded to ~128 chars by the sha256 fallback (64 hex);
// a pathological provider header that's longer is truncated.
func Dedup(provider string, headers map[string]string, body []byte) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if v := headerSpecific(provider, headers); v != "" {
		return clampToken(v)
	}
	// Fallback: sha256 of body. Hex-encoded so the token round-trips
	// through the text column cleanly.
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// headerSpecific returns a provider-supplied delivery id when we know
// where to look. Empty string means "no recognised header — fall back".
//
// Header lookup is case-insensitive: Go's http.Header normalises but
// raw maps from arbitrary sources may not, so we sweep both the
// canonical form and a lower-cased form.
func headerSpecific(provider string, headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	get := func(name string) string {
		if v, ok := headers[name]; ok && v != "" {
			return v
		}
		lower := strings.ToLower(name)
		for k, v := range headers {
			if strings.ToLower(k) == lower && v != "" {
				return v
			}
		}
		return ""
	}

	switch provider {
	case "github":
		return get("X-GitHub-Delivery")
	case "stripe":
		// Stripe-Signature: "t=<ts>,v1=<hex>"
		sig := get("Stripe-Signature")
		if sig == "" {
			return ""
		}
		return stripeDedupFromSignature(sig)
	case "shopify":
		return get("X-Shopify-Webhook-Id")
	}
	return ""
}

// stripeDedupFromSignature derives a stable token from Stripe's
// per-request signature header. The header looks like:
//
//	t=1700000000,v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8bd
//
// On a retry the same (timestamp, signature) pair recurs so we treat
// both as the dedup key. We use the first 16 hex of the v1 value to
// keep the token short while staying unambiguous.
func stripeDedupFromSignature(sig string) string {
	parts := strings.Split(sig, ",")
	var ts, v1 string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "t=") {
			ts = strings.TrimPrefix(p, "t=")
		}
		if strings.HasPrefix(p, "v1=") && v1 == "" {
			v1 = strings.TrimPrefix(p, "v1=")
		}
	}
	if ts == "" && v1 == "" {
		// Header didn't parse — fall back to hashing the whole thing.
		return ""
	}
	if len(v1) > 16 {
		v1 = v1[:16]
	}
	return "stripe:" + ts + ":" + v1
}

// clampToken bounds a provider-supplied identifier so the unique index
// stays compact. 256 chars matches the index's underlying btree page
// budget comfortably.
func clampToken(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max]
}