// SPDX-License-Identifier: Apache-2.0

// Integration test for Recorder.ListMutes under the FORCE-RLS policy on
// suite_notification_mutes.
//
// The bug this guards: CreateMute stores a mute under the concrete
// all-zeros default tenant, but the no-tenant admin listing bound an
// empty tenant WITHOUT opening a bypass-RLS tx. Under FORCE ROW LEVEL
// SECURITY the policy then hides every concrete-tenant row, so the admin
// list came back empty even though the mute exists. ListMutes must mirror
// ListNotifications: bypass RLS only for the no-tenant admin path; keep
// tenant-scoped listing RLS-enforced.
//
// Like retention_test.go this carries no build tag and skips cleanly when
// no database URL is configured, so `go test ./...` stays green in CI
// (which runs no DB) while the live-stack gate exercises it. Postgres
// images often run the DSN user as a superuser (BYPASSRLS implicit), which
// would silently defeat the policy — so the setup routes the Recorder pool
// through a dedicated nosuperuser/nobypassrls role, mirroring the tenancy
// package's RLS harness.
//
// To run against the live dev stack:
//
//	JOBS_TEST_DATABASE_URL='postgres://afstack:afstack@localhost:5499/afstack?sslmode=disable' \
//	  go test ./services/runtime/internal/notifications/... -run TestListMutes -race

package notifications

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const defaultTenantID = "00000000-0000-0000-0000-000000000000"

func mutesTestDBURL() string {
	for _, env := range []string{"JOBS_TEST_DATABASE_URL", "AF_STACK_DATABASE_URL", "DATABASE_URL"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return ""
}

// mutesTestSetup builds an ephemeral schema, a dedicated nosuperuser
// nobypassrls role, applies suite_tenants + suite_notification_mutes with
// the migration-00027 RLS policy, and returns a Recorder whose pool routes
// through the restricted role (with the same PrepareConn tenant binding the
// runtime uses in production).
func mutesTestSetup(t *testing.T) (*Recorder, func()) {
	t.Helper()
	dsn := mutesTestDBURL()
	if dsn == "" {
		t.Skip("set JOBS_TEST_DATABASE_URL (or AF_STACK_DATABASE_URL/DATABASE_URL) to run mutes RLS test")
	}

	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(rb)
	schema := "notif_mutes_test_" + suffix
	role := "notif_mutes_role_" + suffix
	rolePw := "test-" + suffix

	ctx := context.Background()
	bootstrap, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("bootstrap connect: %v", err)
	}
	bootstrapStmts := []string{
		fmt.Sprintf(`create schema %q`, schema),
		`create extension if not exists "pgcrypto"`,
		fmt.Sprintf(`create role %q login password '%s' nosuperuser nobypassrls`, role, rolePw),
		fmt.Sprintf(`grant usage, create on schema %q to %q`, schema, role),
	}
	for _, s := range bootstrapStmts {
		if _, err := bootstrap.Exec(ctx, s); err != nil {
			bootstrap.Close(ctx)
			t.Fatalf("bootstrap stmt %q: %v", mutesFirstLine(s), err)
		}
	}

	// Apply schema + RLS as the privileged bootstrap role, then hand the
	// tables to the restricted role. RLS gates rows, not table GRANTs.
	if _, err := bootstrap.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema)); err != nil {
		bootstrap.Close(ctx)
		t.Fatalf("bootstrap search_path: %v", err)
	}
	ddl := []string{
		`create table suite_tenants (
			id uuid primary key default gen_random_uuid(),
			slug text unique not null,
			name text not null
		)`,
		`insert into suite_tenants (id, slug, name)
		 values ('` + defaultTenantID + `', 'default', 'Default')`,
		// Mirrors migration 00027_block1_admin_endpoints.sql.
		`create table suite_notification_mutes (
			id uuid primary key default gen_random_uuid(),
			tenant_id uuid references suite_tenants(id) on delete cascade,
			kind text not null default '*',
			recipient text not null default '*',
			template text not null default '*',
			category text not null default '*',
			reason text,
			expires_at timestamptz,
			created_by text,
			created_at timestamptz not null default now()
		)`,
		`alter table suite_notification_mutes enable row level security`,
		`alter table suite_notification_mutes force row level security`,
		`create policy tenant_isolation on suite_notification_mutes
		 using (
		   current_setting('app.bypass_rls', true) = 'on'
		   or tenant_id is null
		   or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
		 )
		 with check (
		   current_setting('app.bypass_rls', true) = 'on'
		   or tenant_id is null
		   or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
		 )`,
		fmt.Sprintf(`grant select, insert, update, delete on all tables in schema %q to %q`, schema, role),
	}
	for _, stmt := range ddl {
		if _, err := bootstrap.Exec(ctx, stmt); err != nil {
			bootstrap.Close(ctx)
			t.Fatalf("ddl %q: %v", mutesFirstLine(stmt), err)
		}
	}
	bootstrap.Close(ctx)

	// Build the Recorder pool as the restricted role, with the same
	// per-acquire tenant binding the runtime uses (db.go PrepareConn).
	roleDSN, err := mutesRewriteDSNAuth(dsn, role, rolePw)
	if err != nil {
		t.Fatalf("rewrite dsn: %v", err)
	}
	pcfg, err := pgxpool.ParseConfig(roleDSN)
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema))
		return err
	}
	pcfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		if _, err := conn.Exec(ctx,
			`select set_config('app.tenant_id', $1, false)`,
			tenantctx.TenantID(ctx)); err != nil {
			return false, err
		}
		return true, nil
	}
	pcfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	rec := NewRecorder(pool, slog.Default())

	cleanup := func() {
		pool.Close()
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if conn, err := pgx.Connect(dropCtx, dsn); err == nil {
			_, _ = conn.Exec(dropCtx, fmt.Sprintf(`drop schema if exists %q cascade`, schema))
			_, _ = conn.Exec(dropCtx, fmt.Sprintf(`reassign owned by %q to current_user`, role))
			_, _ = conn.Exec(dropCtx, fmt.Sprintf(`drop owned by %q`, role))
			_, _ = conn.Exec(dropCtx, fmt.Sprintf(`drop role if exists %q`, role))
			conn.Close(dropCtx)
		}
	}
	return rec, cleanup
}

func mutesRewriteDSNAuth(dsn, user, pw string) (string, error) {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dsn, scheme) {
			rest := strings.TrimPrefix(dsn, scheme)
			at := strings.IndexByte(rest, '@')
			if at < 0 {
				return "", fmt.Errorf("dsn missing @host: %s", dsn)
			}
			return scheme + user + ":" + pw + rest[at:], nil
		}
	}
	return "", fmt.Errorf("unsupported dsn scheme: %s", dsn)
}

func mutesFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

// TestListMutesAdminBypassAndTenantIsolation is the regression test for the
// no-tenant admin listing bug. A mute created under the concrete default
// tenant must be visible to the admin (no-tenant) list, while tenant-scoped
// listings stay RLS-isolated: the owning tenant sees it, a different tenant
// does not.
func TestListMutesAdminBypassAndTenantIsolation(t *testing.T) {
	rec, cleanup := mutesTestSetup(t)
	defer cleanup()
	ctx := context.Background()

	// A second, unrelated tenant for the isolation assertion.
	otherTenant := "11111111-1111-1111-1111-111111111111"
	seedCtx := tenantctx.WithTenant(ctx, defaultTenantID, "")
	if _, err := rec.pool.Exec(seedCtx,
		`insert into suite_tenants (id, slug, name) values ($1, 'other', 'Other')`,
		otherTenant); err != nil {
		// suite_tenants has no RLS in this harness, so a bare insert is fine;
		// bind bypass just in case future schema tightens it.
		t.Fatalf("seed other tenant: %v", err)
	}

	// Create a mute under the concrete all-zeros default tenant — exactly
	// what CreateMute does when the handler resolves s.defaultTenant.
	created, err := rec.CreateMute(ctx, CreateMuteInput{
		TenantID: defaultTenantID,
		Pattern:  MutePattern{Kind: "email", Recipient: "*", Template: "*", Category: "*"},
		Reason:   "opted out",
	})
	if err != nil {
		t.Fatalf("create mute: %v", err)
	}
	if created.TenantID == nil || *created.TenantID != defaultTenantID {
		t.Fatalf("created mute tenant = %v, want %s", created.TenantID, defaultTenantID)
	}

	// Admin (no-tenant) listing MUST see the default-tenant mute. This is the
	// behaviour the fix restores; before it, the FORCE-RLS policy hid the row
	// and this returned 0.
	adminList, err := rec.ListMutes(ctx, "")
	if err != nil {
		t.Fatalf("admin list mutes: %v", err)
	}
	if len(adminList.Mutes) != 1 {
		t.Fatalf("admin list returned %d mutes, want 1 (RLS hid the default-tenant row)", len(adminList.Mutes))
	}
	if adminList.Mutes[0].ID != created.ID {
		t.Errorf("admin list returned mute %q, want %q", adminList.Mutes[0].ID, created.ID)
	}

	// Tenant-scoped listing for the owning (default) tenant still sees it.
	ownerList, err := rec.ListMutes(ctx, defaultTenantID)
	if err != nil {
		t.Fatalf("owner list mutes: %v", err)
	}
	if len(ownerList.Mutes) != 1 {
		t.Fatalf("owner list returned %d mutes, want 1", len(ownerList.Mutes))
	}

	// Tenant-scoped listing for a DIFFERENT tenant must NOT see it — RLS
	// isolation is preserved (bypass is scoped to the no-tenant admin path).
	otherList, err := rec.ListMutes(ctx, otherTenant)
	if err != nil {
		t.Fatalf("other-tenant list mutes: %v", err)
	}
	if len(otherList.Mutes) != 0 {
		t.Fatalf("other-tenant list returned %d mutes, want 0 (isolation breach)", len(otherList.Mutes))
	}
}
