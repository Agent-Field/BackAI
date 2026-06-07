// SPDX-License-Identifier: Apache-2.0

package webhooks

import (
	"errors"
	"testing"
)

func TestValidateCreateInputAppliesDefaults(t *testing.T) {
	secret := "ref/github-secret"
	in := CreateEndpointInput{
		Slug:         "github-prs",
		ForwardTo:    "https://example.com/hook",
		SecretKeyRef: &secret,
		Provider:     "github",
	}
	if err := validateCreateInput(&in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.SignatureAlgorithm != "sha256" {
		t.Errorf("expected default sha256 algo, got %q", in.SignatureAlgorithm)
	}
	if in.SignatureHeader != "X-Hub-Signature-256" {
		t.Errorf("expected GitHub-specific header default, got %q", in.SignatureHeader)
	}
	if in.DedupWindowS != 86400 {
		t.Errorf("expected default dedup window 86400, got %d", in.DedupWindowS)
	}
	if in.Provider != "github" {
		t.Errorf("expected provider preserved, got %q", in.Provider)
	}
}

func TestValidateCreateInputRejectsBadSlug(t *testing.T) {
	in := CreateEndpointInput{
		Slug:      "Bad Slug!",
		ForwardTo: "https://example.com/hook",
	}
	err := validateCreateInput(&in)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestValidateCreateInputRejectsBadForward(t *testing.T) {
	in := CreateEndpointInput{
		Slug:      "ok",
		ForwardTo: "ftp://example.com/somewhere",
	}
	err := validateCreateInput(&in)
	if !errors.Is(err, ErrUnsupportedForward) {
		t.Fatalf("expected ErrUnsupportedForward, got %v", err)
	}
}

func TestValidateCreateInputAcceptsAfAgentTarget(t *testing.T) {
	in := CreateEndpointInput{
		Slug:      "ok",
		ForwardTo: "af://agents/my.handler",
	}
	if err := validateCreateInput(&in); err != nil {
		t.Fatalf("af:// target should be accepted: %v", err)
	}
	if in.Provider != "custom" {
		t.Errorf("expected default provider 'custom', got %q", in.Provider)
	}
}

func TestSignatureHeaderForProvider(t *testing.T) {
	cases := map[string]string{
		"github":  "X-Hub-Signature-256",
		"stripe":  "Stripe-Signature",
		"shopify": "X-Shopify-Hmac-Sha256",
		"custom":  "X-Signature",
		"":        "X-Signature",
	}
	for in, want := range cases {
		if got := signatureHeaderForProvider(in); got != want {
			t.Errorf("signatureHeaderForProvider(%q) = %q, want %q", in, got, want)
		}
	}
}