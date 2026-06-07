package webhooks

import (
	"strings"
	"testing"
)

func TestDedupFallsBackToBodyHash(t *testing.T) {
	// No provider-specific header — should use the sha256 of the body.
	a := Dedup("custom", nil, []byte(`{"x":1}`))
	b := Dedup("custom", nil, []byte(`{"x":1}`))
	if a == "" || a != b {
		t.Fatalf("expected deterministic sha256 fallback; a=%q b=%q", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", a)
	}
}

func TestDedupBodyHashChangesWithBody(t *testing.T) {
	a := Dedup("custom", nil, []byte("one"))
	b := Dedup("custom", nil, []byte("two"))
	if a == b {
		t.Fatalf("different bodies should produce different tokens")
	}
}

func TestDedupUsesGitHubDeliveryHeader(t *testing.T) {
	headers := map[string]string{
		"X-GitHub-Delivery": "f8e7d6c5-b4a3-2110-9988-7766554433ff",
	}
	got := Dedup("github", headers, []byte(`{"action":"opened"}`))
	if got != "f8e7d6c5-b4a3-2110-9988-7766554433ff" {
		t.Fatalf("expected GitHub delivery id, got %q", got)
	}
}

func TestDedupCaseInsensitiveHeaderLookup(t *testing.T) {
	headers := map[string]string{
		"x-github-delivery": "lowercase-id",
	}
	got := Dedup("github", headers, []byte(`{}`))
	if got != "lowercase-id" {
		t.Fatalf("expected case-insensitive header match, got %q", got)
	}
}

func TestDedupStripeUsesTimestampAndSignature(t *testing.T) {
	headers := map[string]string{
		"Stripe-Signature": "t=1700000000,v1=5257a869e7ecebed0000000000000000",
	}
	got := Dedup("stripe", headers, []byte(`{}`))
	expected := "stripe:1700000000:5257a869e7ecebed"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestDedupShopifyUsesWebhookID(t *testing.T) {
	headers := map[string]string{
		"X-Shopify-Webhook-Id": "shop-webhook-001",
	}
	got := Dedup("shopify", headers, []byte(`{}`))
	if got != "shop-webhook-001" {
		t.Fatalf("expected shopify webhook id, got %q", got)
	}
}

func TestDedupUnknownProviderUsesBodyHash(t *testing.T) {
	headers := map[string]string{"X-Some-Header": "value"}
	got := Dedup("unknown-provider", headers, []byte("body"))
	if !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("expected sha256 fallback for unknown provider, got %q", got)
	}
}

func TestClampTokenTruncatesAtMax(t *testing.T) {
	huge := strings.Repeat("a", 1024)
	got := clampToken(huge)
	if len(got) != 256 {
		t.Fatalf("expected clamp to 256, got len=%d", len(got))
	}
}

func TestStripeDedupFromSignatureMalformedReturnsEmpty(t *testing.T) {
	got := stripeDedupFromSignature("garbage")
	if got != "" {
		t.Fatalf("expected empty result for malformed sig, got %q", got)
	}
}
