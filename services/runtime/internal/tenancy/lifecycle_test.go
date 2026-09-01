// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the R8 lifecycle manager methods (delete-revokes-keys,
// ownership transfer, soft-delete purge, billing role). Compiled only under
// the `integration` build tag and run against a live Postgres via testSetup;
// excluded from the default `go test` build.

package tenancy

import (
	"context"
	"errors"
	"testing"
)

// TestDeleteTenantRevokesKeys is the R8 acceptance case: soft-deleting a
// tenant must immediately revoke its API keys so they stop authenticating,
// while the rows are retained for the grace period. DB-bound — skips without
// AF_STACK_TEST_DATABASE_URL.
func TestDeleteTenantRevokesKeys(t *testing.T) {
	mgr, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tn, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	issued, err := mgr.IssueKey(ctx, IssueAPIKeyInput{TenantID: tn.ID, Scopes: []string{}})
	if err != nil {
		t.Fatalf("issue key: %v", err)
	}
	// Sanity: the key verifies before deletion.
	if _, err := mgr.VerifyKey(ctx, issued.Value); err != nil {
		t.Fatalf("pre-delete verify: %v", err)
	}

	if err := mgr.DeleteTenant(ctx, tn.ID); err != nil {
		t.Fatalf("delete tenant: %v", err)
	}

	// After deletion the key must be revoked.
	if _, err := mgr.VerifyKey(ctx, issued.Value); !errors.Is(err, ErrKeyRevoked) {
		t.Errorf("post-delete verify = %v, want ErrKeyRevoked", err)
	}
	// Deleting an already-deleted tenant is not found.
	if err := mgr.DeleteTenant(ctx, tn.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("second delete = %v, want ErrTenantNotFound", err)
	}
}

// TestBillingRoleAccepted verifies the widened role set: 'billing' is now a
// valid membership role (migration 00035 widened the CHECK).
func TestBillingRoleAccepted(t *testing.T) {
	mgr, pool, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tn, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "b", Name: "B"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	var userID string
	if err := pool.QueryRow(ctx, `
		insert into suite_users (email, name) values ($1, $2) returning id::text
	`, "billing@example.com", "Billing").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	mem, err := mgr.AddMembership(ctx, tn.ID, userID, "billing")
	if err != nil {
		t.Fatalf("add billing membership: %v", err)
	}
	if mem.Role != "billing" {
		t.Errorf("role = %q, want billing", mem.Role)
	}
	role, err := mgr.MembershipRole(ctx, tn.ID, userID)
	if err != nil {
		t.Fatalf("membership role: %v", err)
	}
	if role != "billing" {
		t.Errorf("MembershipRole = %q, want billing", role)
	}
}

// TestTransferOwnership verifies ownership moves to a member and the prior
// owner is demoted to admin (never left without access), leaving exactly one
// owner.
func TestTransferOwnership(t *testing.T) {
	mgr, pool, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tn, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "org", Name: "Org"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	var owner, member string
	for email, dst := range map[string]*string{"owner@x.com": &owner, "member@x.com": &member} {
		if err := pool.QueryRow(ctx, `
			insert into suite_users (email) values ($1) returning id::text
		`, email).Scan(dst); err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
	}
	if _, err := mgr.AddMembership(ctx, tn.ID, owner, "owner"); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if _, err := mgr.AddMembership(ctx, tn.ID, member, "member"); err != nil {
		t.Fatalf("add member: %v", err)
	}

	// Transfer to a non-member fails.
	if err := mgr.TransferOwnership(ctx, tn.ID, "00000000-0000-0000-0000-0000000000ff"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("transfer to non-member = %v, want ErrUserNotFound", err)
	}

	if err := mgr.TransferOwnership(ctx, tn.ID, member); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if r, _ := mgr.MembershipRole(ctx, tn.ID, member); r != "owner" {
		t.Errorf("new owner role = %q, want owner", r)
	}
	if r, _ := mgr.MembershipRole(ctx, tn.ID, owner); r != "admin" {
		t.Errorf("prior owner role = %q, want admin (demoted)", r)
	}
}

// TestPurgeSoftDeletedTenants covers the grace-period guard and that only
// tenants soft-deleted beyond the grace window are hard-deleted.
func TestPurgeSoftDeletedTenants(t *testing.T) {
	mgr, pool, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	// graceDays must be positive — a misconfig can't purge fresh deletes.
	if _, err := mgr.PurgeSoftDeletedTenants(ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Errorf("grace=0 = %v, want ErrInvalid", err)
	}

	old, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "old", Name: "Old"})
	if err != nil {
		t.Fatalf("create old: %v", err)
	}
	fresh, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "fresh", Name: "Fresh"})
	if err != nil {
		t.Fatalf("create fresh: %v", err)
	}
	// Backdate `old`'s deletion 40 days; `fresh` deleted just now.
	if _, err := pool.Exec(ctx, `update suite_tenants set deleted_at = now() - interval '40 days' where id = $1`, old.ID); err != nil {
		t.Fatalf("backdate old: %v", err)
	}
	if err := mgr.DeleteTenant(ctx, fresh.ID); err != nil {
		t.Fatalf("delete fresh: %v", err)
	}

	purged, err := mgr.PurgeSoftDeletedTenants(ctx, 30)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged < 1 {
		t.Errorf("purged = %d, want >= 1 (the 40-day-old tenant)", purged)
	}
	// `old` is gone; `fresh` (deleted just now) survives.
	if _, err := mgr.GetTenantOnly(ctx, old.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("old tenant should be purged, got %v", err)
	}
	if _, err := mgr.GetTenantOnly(ctx, fresh.ID); err != nil {
		t.Errorf("fresh tenant should survive purge, got %v", err)
	}
}
