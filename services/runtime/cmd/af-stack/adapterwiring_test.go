// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"testing"
)

// fakeCredGetter is a stand-in for *secrets.Vault in resolveCred tests.
type fakeCredGetter struct {
	// values maps the exact key resolveCred looks up -> secret bytes.
	values map[string][]byte
	// lastTenant / lastKey capture what was requested for assertion.
	lastTenant string
	lastKey    string
	err        error
}

func (f *fakeCredGetter) Get(_ context.Context, tenantID, key string) ([]byte, error) {
	f.lastTenant = tenantID
	f.lastKey = key
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.values[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func TestResolveCred_EnvWins(t *testing.T) {
	// Env value present -> returned verbatim (trimmed), vault never consulted.
	g := &fakeCredGetter{values: map[string][]byte{
		"integration/storage/remote_url": []byte("https://vault.example"),
	}}
	got := resolveCredFrom(g, "storage", "remote_url", "  https://env.example  ")
	if got != "https://env.example" {
		t.Fatalf("env should win and be trimmed, got %q", got)
	}
	if g.lastKey != "" {
		t.Fatalf("vault must not be consulted when env is set; looked up %q", g.lastKey)
	}
}

func TestResolveCred_VaultFallback(t *testing.T) {
	g := &fakeCredGetter{values: map[string][]byte{
		"integration/notifications/slack_webhook_url": []byte(" https://hooks.slack.com/x \n"),
	}}
	got := resolveCredFrom(g, "notifications", "slack_webhook_url", "")
	if got != "https://hooks.slack.com/x" {
		t.Fatalf("expected trimmed vault value, got %q", got)
	}
	if g.lastKey != "integration/notifications/slack_webhook_url" {
		t.Fatalf("unexpected vault key %q", g.lastKey)
	}
	if g.lastTenant != integrationCredTenant {
		t.Fatalf("resolveCred must read the integration tenant %q, got %q", integrationCredTenant, g.lastTenant)
	}
}

func TestResolveCred_NilVault(t *testing.T) {
	// Nil concrete vault + empty env -> "" (never panics on the typed-nil).
	if got := resolveCred(nil, "storage", "remote_url", ""); got != "" {
		t.Fatalf("nil vault + no env should yield empty, got %q", got)
	}
	// Nil vault but env set -> env still wins.
	if got := resolveCred(nil, "storage", "remote_url", "https://env"); got != "https://env" {
		t.Fatalf("nil vault + env should yield env, got %q", got)
	}
	// Nil getter through the internal helper is also safe.
	if got := resolveCredFrom(nil, "storage", "remote_url", ""); got != "" {
		t.Fatalf("nil getter should yield empty, got %q", got)
	}
}

func TestResolveCred_VaultMissOrError(t *testing.T) {
	// Miss -> "".
	g := &fakeCredGetter{values: map[string][]byte{}}
	if got := resolveCredFrom(g, "llm", "remote_token", ""); got != "" {
		t.Fatalf("vault miss should yield empty, got %q", got)
	}
	// Lookup error -> "" (defensive; never surfaces the error).
	ge := &fakeCredGetter{err: errors.New("boom")}
	if got := resolveCredFrom(ge, "llm", "remote_token", ""); got != "" {
		t.Fatalf("vault error should yield empty, got %q", got)
	}
}
