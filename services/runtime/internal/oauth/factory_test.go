// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"errors"
	"testing"
)

func TestFactoryUnconfiguredProvider(t *testing.T) {
	// Clear env so no provider is configured.
	for _, n := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv("OAUTH_"+n+"_CLIENT_ID", "")
		t.Setenv("OAUTH_"+n+"_CLIENT_SECRET", "")
	}
	f := NewFactoryFromEnv()
	if _, err := f.Get("google"); !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
	}
	if _, err := f.Get("bogus"); !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("expected ErrProviderUnknown, got %v", err)
	}
	if f.IsConfigured("google") {
		t.Fatalf("google should be unconfigured")
	}
	list := f.List()
	if len(list) != len(AllProviderNames) {
		t.Fatalf("List length mismatch: got %d want %d", len(list), len(AllProviderNames))
	}
	for _, info := range list {
		if info.Configured {
			t.Fatalf("provider %q reported configured with no env vars", info.Name)
		}
		if len(info.DefaultScopes) == 0 {
			t.Fatalf("provider %q has empty default scopes", info.Name)
		}
	}
}

func TestFactoryConfiguredGoogle(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "")
	f := NewFactoryFromEnv()
	p, err := f.Get("google")
	if err != nil {
		t.Fatalf("Get(google): %v", err)
	}
	if p.Name() != "google" {
		t.Fatalf("Provider name = %q", p.Name())
	}
	if !f.IsConfigured("google") {
		t.Fatalf("expected google configured")
	}
	if f.IsConfigured("github") {
		t.Fatalf("expected github NOT configured")
	}
}

func TestProviderAuthorizeURLContainsExpected(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "gid")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "gsec")
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "ghid")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "ghsec")
	f := NewFactoryFromEnv()

	gp, _ := f.Get("google")
	u := gp.AuthorizeURL("state123", []string{"scope_a", "scope_b"}, "http://localhost:8080/cb")
	must := []string{"client_id=gid", "redirect_uri=http", "state=state123", "scope=scope_a"}
	for _, s := range must {
		if !contains(u, s) {
			t.Fatalf("google AuthorizeURL missing %q in %q", s, u)
		}
	}

	gh, _ := f.Get("github")
	u = gh.AuthorizeURL("st", nil, "http://localhost:8080/cb")
	must = []string{"client_id=ghid", "state=st", "scope=repo"}
	for _, s := range must {
		if !contains(u, s) {
			t.Fatalf("github AuthorizeURL missing %q in %q", s, u)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
