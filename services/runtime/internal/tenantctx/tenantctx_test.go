// SPDX-License-Identifier: Apache-2.0

package tenantctx

import (
	"context"
	"testing"
)

func TestWithTenantRoundTrip(t *testing.T) {
	ctx := WithTenant(context.Background(), "tenant-a", "key-1")
	if got := TenantID(ctx); got != "tenant-a" {
		t.Errorf("TenantID = %q, want tenant-a", got)
	}
	if got := APIKeyID(ctx); got != "key-1" {
		t.Errorf("APIKeyID = %q, want key-1", got)
	}
}

func TestEmptyContextReturnsEmptyStrings(t *testing.T) {
	ctx := context.Background()
	if got := TenantID(ctx); got != "" {
		t.Errorf("TenantID empty ctx = %q, want empty", got)
	}
	if got := APIKeyID(ctx); got != "" {
		t.Errorf("APIKeyID empty ctx = %q, want empty", got)
	}
}

func TestNilContextSafe(t *testing.T) {
	// Defensive: a nil ctx should never panic in helpers.
	if got := TenantID(nil); got != "" { //nolint:staticcheck
		t.Errorf("TenantID(nil) = %q, want empty", got)
	}
	if got := APIKeyID(nil); got != "" { //nolint:staticcheck
		t.Errorf("APIKeyID(nil) = %q, want empty", got)
	}
}

func TestWithTenantSkipsEmpty(t *testing.T) {
	// If a caller passes an empty tenantID, we don't want to overwrite
	// a value that might have been set earlier in the middleware chain.
	base := WithTenant(context.Background(), "tenant-a", "key-1")
	derived := WithTenant(base, "", "")
	if got := TenantID(derived); got != "tenant-a" {
		t.Errorf("expected tenant retained after empty WithTenant, got %q", got)
	}
	if got := APIKeyID(derived); got != "key-1" {
		t.Errorf("expected api key retained after empty WithTenant, got %q", got)
	}
}

func TestWithTenantNilContextSafe(t *testing.T) {
	ctx := WithTenant(nil, "t", "k") //nolint:staticcheck
	if TenantID(ctx) != "t" || APIKeyID(ctx) != "k" {
		t.Errorf("expected nil-ctx upgrade to background; got tenant=%q key=%q",
			TenantID(ctx), APIKeyID(ctx))
	}
}