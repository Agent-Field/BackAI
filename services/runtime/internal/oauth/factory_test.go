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

// resolverFrom builds a CredentialResolver over an in-memory map keyed by
// "<provider>/<field>". Mutating the map between calls simulates an
// operator saving credentials at runtime.
func resolverFrom(store map[string]string) CredentialResolver {
	return func(provider, field string) (string, bool) {
		v, ok := store[provider+"/"+field]
		return v, ok
	}
}

// TestFactoryVaultFirstResolution covers vault-backed resolution and the
// env fallback boundary: no creds ⇒ not configured; vault creds ⇒
// configured with source "vault".
func TestFactoryVaultFirstResolution(t *testing.T) {
	for _, n := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv("OAUTH_"+n+"_CLIENT_ID", "")
		t.Setenv("OAUTH_"+n+"_CLIENT_SECRET", "")
	}
	vault := map[string]string{}
	f := NewFactoryFromEnv()
	f.SetCredentialResolver(resolverFrom(vault))

	// Nothing configured yet.
	if _, err := f.Get("google"); !errors.Is(err, ErrProviderNotConfigured) {
		t.Fatalf("expected ErrProviderNotConfigured, got %v", err)
	}
	for _, info := range f.List() {
		if info.Name == "google" {
			if info.Configured || info.CredentialsSource != "" {
				t.Fatalf("google should be unconfigured with empty source, got %+v", info)
			}
		}
	}

	// Operator saves creds — no new factory, no restart.
	vault["google/client_id"] = "vault-id"
	vault["google/client_secret"] = "vault-secret"

	p, err := f.Get("google")
	if err != nil {
		t.Fatalf("Get(google) after save: %v", err)
	}
	if p.Name() != "google" {
		t.Fatalf("provider name = %q", p.Name())
	}
	if !f.IsConfigured("google") {
		t.Fatal("google should be configured after vault save")
	}
	var googleInfo ProviderInfo
	for _, info := range f.List() {
		if info.Name == "google" {
			googleInfo = info
		}
	}
	if !googleInfo.Configured || googleInfo.CredentialsSource != "vault" {
		t.Fatalf("expected configured google with source vault, got %+v", googleInfo)
	}
	// AuthorizeURL must carry the vault client id — proves the adapter was
	// built from the resolved credential.
	if u := p.AuthorizeURL("st", nil, "http://localhost/cb"); !contains(u, "client_id=vault-id") {
		t.Fatalf("authorize URL missing vault client id: %q", u)
	}
}

// TestFactoryVaultWinsOverEnv pins precedence: when both env and vault
// carry creds, vault wins and source is "vault".
func TestFactoryVaultWinsOverEnv(t *testing.T) {
	t.Setenv("OAUTH_GOOGLE_CLIENT_ID", "env-id")
	t.Setenv("OAUTH_GOOGLE_CLIENT_SECRET", "env-secret")
	f := NewFactoryFromEnv()

	// Env only first.
	p, err := f.Get("google")
	if err != nil {
		t.Fatalf("Get(google) env: %v", err)
	}
	if u := p.AuthorizeURL("st", nil, "cb"); !contains(u, "client_id=env-id") {
		t.Fatalf("expected env client id, got %q", u)
	}
	var info ProviderInfo
	for _, i := range f.List() {
		if i.Name == "google" {
			info = i
		}
	}
	if info.CredentialsSource != "env" {
		t.Fatalf("expected source env, got %q", info.CredentialsSource)
	}

	// Now a vault credential shadows env.
	f.SetCredentialResolver(resolverFrom(map[string]string{
		"google/client_id":     "vault-id",
		"google/client_secret": "vault-secret",
	}))
	p, err = f.Get("google")
	if err != nil {
		t.Fatalf("Get(google) vault: %v", err)
	}
	if u := p.AuthorizeURL("st", nil, "cb"); !contains(u, "client_id=vault-id") {
		t.Fatalf("expected vault client id to win, got %q", u)
	}
	for _, i := range f.List() {
		if i.Name == "google" && i.CredentialsSource != "vault" {
			t.Fatalf("expected source vault, got %q", i.CredentialsSource)
		}
	}
}

// TestFactoryCacheRebuildsOnCredChange verifies adapters are memoised for
// unchanged creds but rebuilt when the resolved creds change.
func TestFactoryCacheRebuildsOnCredChange(t *testing.T) {
	vault := map[string]string{
		"github/client_id":     "id1",
		"github/client_secret": "sec1",
	}
	f := NewFactoryFromEnv()
	f.SetCredentialResolver(resolverFrom(vault))

	p1, err := f.Get("github")
	if err != nil {
		t.Fatalf("Get github: %v", err)
	}
	p2, _ := f.Get("github")
	if p1 != p2 {
		t.Fatal("expected memoised adapter for unchanged creds")
	}

	// Change the secret — adapter must be rebuilt (different instance).
	vault["github/client_secret"] = "sec2"
	p3, _ := f.Get("github")
	if p3 == p1 {
		t.Fatal("expected a rebuilt adapter after credential change")
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
