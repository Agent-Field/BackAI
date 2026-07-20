// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"errors"
	"net/http"
	"testing"
)

func mustRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://api.acme.test/x", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

// Contract: the default registry ships github, stripe, google, and slack,
// each with a coherent capability descriptor.
func TestDefaultRegistryProviders(t *testing.T) {
	reg := DefaultRegistry()
	for _, name := range []string{"github", "stripe", "google", "slack"} {
		if _, err := reg.Get(name); err != nil {
			t.Fatalf("provider %q missing: %v", name, err)
		}
	}
	if _, err := reg.Get("does-not-exist"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}

// Contract: descriptors declare the right auth kind and OAuth capability.
func TestDescriptorCapabilities(t *testing.T) {
	reg := DefaultRegistry()

	stripe, _ := reg.Get("stripe")
	if stripe.Kind != KindAPIKey {
		t.Fatalf("stripe should be api_key, got %q", stripe.Kind)
	}
	if stripe.OAuthCapable() {
		t.Fatal("stripe should not be OAuth-capable")
	}
	if stripe.WebhookScheme != WebhookStripe {
		t.Fatalf("stripe webhook scheme = %q", stripe.WebhookScheme)
	}

	github, _ := reg.Get("github")
	if github.Kind != KindOAuth || !github.OAuthCapable() {
		t.Fatal("github should be OAuth-capable")
	}
	if github.WebhookScheme != WebhookGitHubHMAC {
		t.Fatalf("github webhook scheme = %q", github.WebhookScheme)
	}

	for _, name := range []string{"google", "slack"} {
		d, _ := reg.Get(name)
		if !d.OAuthCapable() {
			t.Fatalf("%s should be OAuth-capable", name)
		}
		if d.TokenEndpoint == "" || d.AuthorizeEndpoint == "" {
			t.Fatalf("%s missing oauth endpoints", name)
		}
	}
}

// Contract: Names returns a stable, sorted provider list.
func TestRegistryNamesSorted(t *testing.T) {
	names := DefaultRegistry().Names()
	want := []string{"github", "google", "slack", "stripe"}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names not sorted: %v", names)
		}
	}
}

// Contract: descriptor.injectAuth overwrites any existing auth header — app
// code cannot supply its own credential.
func TestInjectAuthOverwrites(t *testing.T) {
	d, _ := DefaultRegistry().Get("github")
	req := mustRequest(t)
	req.Header.Set("Authorization", "Bearer attacker-token")
	d.injectAuth(req, "real-token")
	if got := req.Header.Get("Authorization"); got != "Bearer real-token" {
		t.Fatalf("auth header = %q, want injected value", got)
	}
}

// Contract: resolveURL joins base + relative path and refuses an absolute URL.
func TestResolveURL(t *testing.T) {
	d := Descriptor{BaseURL: "https://api.acme.test"}
	got, err := d.resolveURL("/v1/things", map[string]string{"limit": "10"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "https://api.acme.test/v1/things?limit=10" {
		t.Fatalf("resolved = %q", got)
	}
	if _, err := d.resolveURL("https://evil.test/x", nil); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
	if _, err := d.resolveURL("//evil.test/x", nil); err == nil {
		t.Fatal("expected protocol-relative path to be rejected")
	}
}
