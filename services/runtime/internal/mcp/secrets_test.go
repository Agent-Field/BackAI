// SPDX-License-Identifier: Apache-2.0

// secrets_test.go — unit tests for the env-resolver. No DB; uses a
// hand-rolled SecretReader stub so the logic is exercised end-to-end
// without pgxpool.
package mcp

import (
	"context"
	"errors"
	"testing"
)

type fakeVault struct {
	rows map[string][]byte
	err  error
}

func (f *fakeVault) Get(_ context.Context, _ string, key string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.rows[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func TestResolveEnv_PassThroughNonSecretValues(t *testing.T) {
	in := map[string]string{"FOO": "bar", "DEBUG": "1"}
	out, err := ResolveEnv(context.Background(), in, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["FOO"] != "bar" || out["DEBUG"] != "1" {
		t.Fatalf("non-secret values mangled: %#v", out)
	}
	// returned map is a fresh allocation
	out["FOO"] = "mutated"
	if in["FOO"] == "mutated" {
		t.Fatalf("ResolveEnv leaked aliasing to caller's map")
	}
}

func TestResolveEnv_ResolvesSecretPrefix(t *testing.T) {
	vault := &fakeVault{rows: map[string][]byte{
		"github.token": []byte("ghp_real"),
	}}
	in := map[string]string{
		"GITHUB_TOKEN": "secret:github.token",
		"PUBLIC":       "ok",
	}
	out, err := ResolveEnv(context.Background(), in, vault, "tenant-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["GITHUB_TOKEN"] != "ghp_real" {
		t.Fatalf("secret not resolved; got %q", out["GITHUB_TOKEN"])
	}
	if out["PUBLIC"] != "ok" {
		t.Fatalf("non-secret value lost; got %q", out["PUBLIC"])
	}
}

func TestResolveEnv_NilVaultFailsForSecretRef(t *testing.T) {
	in := map[string]string{"K": "secret:x"}
	_, err := ResolveEnv(context.Background(), in, nil, "")
	if err == nil {
		t.Fatalf("expected error when vault is nil and value is secret-ref")
	}
	if !errors.Is(err, ErrSecretLookupFailed) {
		t.Fatalf("expected ErrSecretLookupFailed, got %v", err)
	}
}

func TestResolveEnv_EmptyKeyAfterPrefixRejected(t *testing.T) {
	vault := &fakeVault{rows: map[string][]byte{}}
	in := map[string]string{"K": "secret:"}
	_, err := ResolveEnv(context.Background(), in, vault, "")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty secret key, got %v", err)
	}
}

func TestResolveEnv_PropagatesVaultError(t *testing.T) {
	vault := &fakeVault{err: errors.New("boom")}
	in := map[string]string{"K": "secret:x"}
	_, err := ResolveEnv(context.Background(), in, vault, "")
	if !errors.Is(err, ErrSecretLookupFailed) {
		t.Fatalf("expected ErrSecretLookupFailed, got %v", err)
	}
}

func TestValidateName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"github", true},
		{"github-mcp", true},
		{"git123", true},
		{"", false},
		{"Github", false},  // uppercase rejected
		{"git_hub", false}, // underscore rejected
		{"git.hub", false}, // dot rejected
	}
	for _, c := range cases {
		err := validateName(c.name)
		if c.ok && err != nil {
			t.Errorf("validateName(%q) unexpected error: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("validateName(%q) expected error, got nil", c.name)
		}
	}
}

func TestPool_TenantMatches(t *testing.T) {
	rowA := "tenant-a"
	cases := []struct {
		row    *string
		caller string
		want   bool
	}{
		{nil, "", true},          // admin view, shared row
		{nil, "tenant-a", true},  // tenant view, shared row
		{&rowA, "", true},        // admin view sees per-tenant row
		{&rowA, "tenant-a", true}, // matching tenant
		{&rowA, "tenant-b", false}, // mismatching tenant
	}
	for i, c := range cases {
		got := tenantMatches(c.row, c.caller)
		if got != c.want {
			t.Errorf("case %d: tenantMatches(%v, %q) = %v, want %v", i, c.row, c.caller, got, c.want)
		}
	}
}