// SPDX-License-Identifier: Apache-2.0

// Pure-Go unit tests that don't need Postgres. These run on every
// `go test ./...` so the token parser + sentinel-error wiring keep
// passing even when the integration DB isn't available.
//
// The DB-backed flows (RLS isolation, bcrypt round-trip, slug
// uniqueness, membership CRUD) live in tenancy_test.go behind
// `//go:build integration`.

package tenancy

import (
	"errors"
	"strings"
	"testing"
)

// TestParseTokenAccepts verifies parseToken extracts prefix+secret for
// well-formed tokens of the documented `af_<prefix>_<secret>` shape.
func TestParseTokenAccepts(t *testing.T) {
	tok := "af_abc123_" + strings.Repeat("z", 32)
	prefix, secret, err := parseToken(tok)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if prefix != "abc123" {
		t.Errorf("prefix = %q, want abc123", prefix)
	}
	if secret != strings.Repeat("z", 32) {
		t.Errorf("secret length = %d, want 32", len(secret))
	}
}

// TestParseTokenRejects exercises the four malformed-token paths the
// resolver maps to 401.
func TestParseTokenRejects(t *testing.T) {
	for _, bad := range []string{
		"",                    // empty
		"af_",                 // no separator past prefix marker
		"af__secret",          // empty prefix
		"af_prefix_",          // empty secret
		"notaf_prefix_secret", // missing token prefix marker
		"prefix_secret",       // missing `af_` entirely
	} {
		_, _, err := parseToken(bad)
		if !errors.Is(err, ErrInvalidAPIKeyFormat) {
			t.Errorf("parseToken(%q) = %v, want ErrInvalidAPIKeyFormat", bad, err)
		}
	}
}

// TestSentinelWrappingMatches confirms each fine-grained sentinel
// matches its coarse bucket via errors.Is so the admin handler's
// switch statement maps them to the right HTTP status.
func TestSentinelWrappingMatches(t *testing.T) {
	checks := []struct {
		err  error
		base error
	}{
		{ErrTenantNotFound, ErrNotFound},
		{ErrUserNotFound, ErrNotFound},
		{ErrMembershipNotFound, ErrNotFound},
		{ErrAPIKeyNotFound, ErrNotFound},
		{ErrSlugTaken, ErrConflict},
		{ErrInvalidSlug, ErrInvalid},
		{ErrInvalidRole, ErrInvalid},
		{ErrInvalidAPIKeyFormat, ErrInvalid},
	}
	for _, c := range checks {
		if !errors.Is(c.err, c.base) {
			t.Errorf("%v should wrap %v", c.err, c.base)
		}
	}

	// And the leaf sentinels MUST NOT wrap each other (they're peers).
	if errors.Is(ErrTenantNotFound, ErrAPIKeyNotFound) {
		t.Error("ErrTenantNotFound must not match ErrAPIKeyNotFound — they're peers")
	}
}

// TestRandomTokenEntropyAndShape sanity-checks the base32 token helper:
// each call returns a fresh, lower-case, padding-free string.
func TestRandomTokenEntropyAndShape(t *testing.T) {
	a, err := randomToken(keyPrefixBytes)
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomToken(keyPrefixBytes)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("random tokens must differ between calls")
	}
	if strings.Contains(a, "=") {
		t.Errorf("token should be padding-free, got %q", a)
	}
	if strings.ToLower(a) != a {
		t.Errorf("token should be lower-case, got %q", a)
	}
}
