// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

func TestFeatureFlagContextDefaultsTenant(t *testing.T) {
	s := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/config/flags", nil)

	ctx := s.featureFlagContext(context.Background(), req)
	if got := tenantctx.TenantID(ctx); got != tenancy.DefaultTenantID {
		t.Fatalf("tenant id = %q, want default %q", got, tenancy.DefaultTenantID)
	}
}

func TestFeatureFlagContextPreservesResolvedTenant(t *testing.T) {
	s := New(config.Default(), slog.Default(), Deps{})
	req := httptest.NewRequest("GET", "/api/v1/config/flags", nil)
	base := tenantctx.WithTenantAndUser(context.Background(), "tenant-a", "key-a", "user-a")

	ctx := s.featureFlagContext(base, req)
	if got := tenantctx.TenantID(ctx); got != "tenant-a" {
		t.Fatalf("tenant id = %q, want tenant-a", got)
	}
	if got := tenantctx.UserID(ctx); got != "user-a" {
		t.Fatalf("user id = %q, want user-a", got)
	}
}
