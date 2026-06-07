// SPDX-License-Identifier: Apache-2.0

//go:build integration

// Integration tests for the tenancy package.
//
// These run against a live Postgres and exercise:
//
//   - RLS isolation (the headline guarantee of Phase 6.1): two tenants,
//     one inserts a secret, the other can't see it after switching the
//     `app.tenant_id` GUC.
//   - bcrypt round-trip on API keys (issue -> verify -> revoke ->
//     ErrKeyRevoked).
//   - slug uniqueness (CreateTenant twice with the same slug returns
//     ErrSlugTaken; the error wraps ErrConflict for handler mapping).
//   - the admin escape hatch (`app.bypass_rls`) restores cross-tenant
//     visibility for admin queries.
//
// Each test gets its own ephemeral schema so two runs against the same
// DB don't collide. The schema is dropped on test completion.
//
// To run:
//
//	docker run --rm -d --name af-tenancy-pg \
//	  -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:17-alpine
//	export JOBS_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable'
//	go test -tags=integration ./services/runtime/internal/tenancy/...
//
// We re-use the JOBS_TEST_DATABASE_URL env name (already documented in
// jobs_test.go) so the CI matrix doesn't grow another variable.

package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
)

const databaseURLEnv = "JOBS_TEST_DATABASE_URL"

// testSetup builds an ephemeral schema, applies the Phase 6.1 schema
// inline (we don't pull in package `db` here because that would create
// an import cycle through embed.FS) and returns a started Manager.
//
// The inline schema is a minimal subset of migrations 00001 + 00003 +
// 00004 — enough to exercise RLS on suite_secrets without dragging
// better-auth or River into the test boundary.
//
// IMPORTANT: many local Postgres images run the test user as superuser
// (and therefore with BYPASSRLS implicit). Even `FORCE ROW LEVEL
// SECURITY` doesn't apply to BYPASSRLS roles. So the setup creates a
// dedicated non-superuser role and routes the test pool through it —
// otherwise the RLS isolation test would silently pass by bypassing
// the very thing it's meant to verify.
func testSetup(t *testing.T) (*Manager, *pgxpool.Pool, func()) {
	t.Helper()
	dsn := os.Getenv(databaseURLEnv)
	if dsn == "" {
		t.Skipf("set %s to run tenancy integration tests", databaseURLEnv)
	}

	// Each test gets its own schema + role. Using hex (not base32) for
	// the suffix keeps the identifier within Postgres' 63-char limit.
	rb := make([]byte, 6)
	if _, err := rand.Read(rb); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(rb)
	schema := "tenancy_test_" + suffix
	role := "tenancy_role_" + suffix
	rolePw := "test-" + suffix

	ctx := context.Background()
	bootstrap, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("bootstrap connect: %v", err)
	}

	// Create the schema, the pgcrypto extension (idempotent), and a
	// dedicated non-superuser role with usage on the schema.
	bootstrapStmts := []string{
		fmt.Sprintf(`create schema %q`, schema),
		`create extension if not exists "pgcrypto"`,
		fmt.Sprintf(`create role %q login password '%s' nosuperuser nobypassrls`, role, rolePw),
		fmt.Sprintf(`grant usage, create on schema %q to %q`, schema, role),
		fmt.Sprintf(`alter default privileges in schema %q
			grant select, insert, update, delete on tables to %q`, schema, role),
	}
	for _, s := range bootstrapStmts {
		if _, err := bootstrap.Exec(ctx, s); err != nil {
			bootstrap.Close(ctx)
			t.Fatalf("bootstrap stmt %q: %v", firstLine(s), err)
		}
	}
	bootstrap.Close(ctx)

	// Apply schema as the SUPERUSER (the DSN's user). RLS attaches to
	// the table, so the policy applies to subsequent reads from the
	// non-superuser role.
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	adminConn, err := adminPool.Acquire(ctx)
	if err != nil {
		adminPool.Close()
		t.Fatalf("acquire admin: %v", err)
	}
	if _, err := adminConn.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema)); err != nil {
		adminConn.Release()
		adminPool.Close()
		t.Fatalf("admin search_path: %v", err)
	}
	if err := applyTestSchema(ctx, adminConn.Conn()); err != nil {
		adminConn.Release()
		adminPool.Close()
		t.Fatalf("apply schema: %v", err)
	}
	// Hand all created tables to the role so it has full access — RLS
	// is what gates rows, not table-level GRANTs.
	if _, err := adminConn.Exec(ctx, fmt.Sprintf(
		`grant select, insert, update, delete on all tables in schema %q to %q`,
		schema, role)); err != nil {
		adminConn.Release()
		adminPool.Close()
		t.Fatalf("grant tables: %v", err)
	}
	adminConn.Release()
	adminPool.Close()

	// Build the test pool against the dedicated non-superuser role.
	roleDSN, err := rewriteDSNAuth(dsn, role, rolePw)
	if err != nil {
		t.Fatalf("rewrite dsn: %v", err)
	}
	pcfg, err := pgxpool.ParseConfig(roleDSN)
	if err != nil {
		t.Fatalf("parse pool: %v", err)
	}
	// Force every pool connection onto the test schema. We DON'T set
	// app.tenant_id at connect time — RLS-bypass-by-omission is the
	// foot-gun we want our tests to catch.
	pcfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, fmt.Sprintf(`set search_path to %q`, schema))
		return err
	}
	pcfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	mgr, err := New(pool, hooks.NewEngine(slog.Default()), slog.Default())
	if err != nil {
		pool.Close()
		t.Fatalf("new manager: %v", err)
	}

	cleanup := func() {
		pool.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if conn, err := pgx.Connect(dropCtx, dsn); err == nil {
			_, _ = conn.Exec(dropCtx,
				fmt.Sprintf(`drop schema if exists %q cascade`, schema))
			// Revoke and drop the role — ignore errors so cleanup is
			// best-effort.
			_, _ = conn.Exec(dropCtx,
				fmt.Sprintf(`reassign owned by %q to current_user`, role))
			_, _ = conn.Exec(dropCtx,
				fmt.Sprintf(`drop owned by %q`, role))
			_, _ = conn.Exec(dropCtx, fmt.Sprintf(`drop role if exists %q`, role))
			conn.Close(dropCtx)
		}
	}
	return mgr, pool, cleanup
}

// rewriteDSNAuth swaps the user:password segment of a Postgres DSN with
// `user`:`pw`. Supports both `postgres://user:pw@...` and standalone
// `postgresql://user@...` forms. Doesn't try to be exhaustive — only
// covers the URL shapes our tests use.
func rewriteDSNAuth(dsn, user, pw string) (string, error) {
	for _, scheme := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dsn, scheme) {
			rest := strings.TrimPrefix(dsn, scheme)
			at := strings.IndexByte(rest, '@')
			if at < 0 {
				return "", fmt.Errorf("dsn missing @host: %s", dsn)
			}
			tail := rest[at:]
			return scheme + user + ":" + pw + tail, nil
		}
	}
	return "", fmt.Errorf("unsupported dsn scheme: %s", dsn)
}

// applyTestSchema creates the subset of tables the tenancy tests touch
// and applies the RLS policy on suite_secrets. It mirrors migrations
// 00001 + 00003 + 00004 but skips suite_gateway_requests and
// suite_audit_log to keep the test boundary tight (RLS semantics are
// identical across all three policed tables).
//
// Takes a *pgx.Conn (not a pool) so the caller can apply schema via the
// privileged bootstrap role and then hand the tables off to the
// unprivileged test role.
func applyTestSchema(ctx context.Context, conn *pgx.Conn) error {
	ddl := []string{
		`create table suite_tenants (
			id uuid primary key default gen_random_uuid(),
			slug text unique not null,
			name text not null,
			plan text default 'free',
			settings jsonb default '{}'::jsonb,
			quota jsonb default '{}'::jsonb,
			created_at timestamptz not null default now(),
			deleted_at timestamptz
		)`,
		`insert into suite_tenants (id, slug, name)
		 values ('00000000-0000-0000-0000-000000000000', 'default', 'Default')`,
		`create table suite_users (
			id uuid primary key default gen_random_uuid(),
			email text unique not null,
			name text,
			avatar_url text,
			created_at timestamptz not null default now(),
			deleted_at timestamptz
		)`,
		`create table suite_memberships (
			tenant_id uuid references suite_tenants(id) on delete cascade,
			user_id uuid references suite_users(id) on delete cascade,
			role text not null check (role in ('owner','admin','member','viewer')),
			invited_at timestamptz not null default now(),
			accepted_at timestamptz,
			primary key (tenant_id, user_id)
		)`,
		`create table suite_api_keys (
			id uuid primary key default gen_random_uuid(),
			tenant_id uuid references suite_tenants(id) on delete cascade,
			prefix text unique not null,
			hashed_secret text not null,
			name text,
			scopes text[] not null default '{}',
			created_by uuid references suite_users(id),
			created_at timestamptz not null default now(),
			last_used_at timestamptz,
			expires_at timestamptz,
			revoked_at timestamptz
		)`,
		`create table suite_gateway_requests (
			id uuid primary key default gen_random_uuid(),
			tenant_id uuid not null default '00000000-0000-0000-0000-000000000000',
			endpoint text not null,
			method text not null,
			created_at timestamptz not null default now()
		)`,
		`create table suite_secrets (
			tenant_id uuid not null references suite_tenants(id) on delete cascade,
			key text not null,
			encrypted_value bytea not null,
			kms_key_id text not null default 'v1',
			metadata jsonb not null default '{}'::jsonb,
			rotate_after timestamptz,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key (tenant_id, key)
		)`,
		// RLS on suite_secrets — same shape as migration 00004.
		`alter table suite_secrets enable row level security`,
		`alter table suite_secrets force row level security`,
		`create policy tenant_isolation on suite_secrets
		 using (
		   current_setting('app.bypass_rls', true) = 'on'
		   or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
		 )
		 with check (
		   current_setting('app.bypass_rls', true) = 'on'
		   or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
		 )`,
	}
	for _, stmt := range ddl {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ddl %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

// ─── Tests ────────────────────────────────────────────────────────────────

// TestCreateTenantSlugUniqueness exercises the happy path then verifies
// the second create with the same slug returns ErrSlugTaken (which
// wraps ErrConflict for HTTP 409).
func TestCreateTenantSlugUniqueness(t *testing.T) {
	mgr, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	first, err := mgr.CreateTenant(ctx, CreateTenantInput{
		Slug: "acme", Name: "Acme", Plan: "pro",
	})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.Slug != "acme" || first.Plan != "pro" {
		t.Errorf("unexpected tenant: %+v", first)
	}
	if first.Settings == nil || first.Quota == nil {
		t.Errorf("settings/quota must be non-nil maps for JSON round-trip")
	}

	_, err = mgr.CreateTenant(ctx, CreateTenantInput{Slug: "acme", Name: "Acme 2"})
	if !errors.Is(err, ErrSlugTaken) {
		t.Fatalf("expected ErrSlugTaken, got %v", err)
	}
	if !errors.Is(err, ErrConflict) {
		t.Errorf("ErrSlugTaken must wrap ErrConflict, got %v", err)
	}

	// Invalid slug should reject before hitting the DB.
	_, err = mgr.CreateTenant(ctx, CreateTenantInput{Slug: "BadSlug!", Name: "bad"})
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug for bad charset, got %v", err)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("ErrInvalidSlug must wrap ErrInvalid")
	}
}

// TestRLSIsolation is the headline guarantee: two tenants, two secrets,
// switching app.tenant_id hides the other tenant's row.
func TestRLSIsolation(t *testing.T) {
	mgr, pool, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tA, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "tenant-a", Name: "A"})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	tB, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "tenant-b", Name: "B"})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Insert one secret per tenant. We acquire a pooled conn, set
	// app.tenant_id transactionally (via the resolver helper), then
	// insert. This proves the WITH CHECK clause accepts the insert
	// when tenant_id matches the GUC.
	insertSecret := func(tenantID, key string) error {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		if _, err := tx.Exec(ctx,
			`select set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			insert into suite_secrets (tenant_id, key, encrypted_value)
			values ($1, $2, $3)
		`, tenantID, key, []byte("ciphertext")); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return tx.Commit(ctx)
	}
	if err := insertSecret(tA.ID, "openai-key"); err != nil {
		t.Fatalf("insert secret A: %v", err)
	}
	if err := insertSecret(tB.ID, "anthropic-key"); err != nil {
		t.Fatalf("insert secret B: %v", err)
	}

	// Read as A: expect exactly one row.
	countAs := func(tenantID string) (int, error) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return 0, err
		}
		defer conn.Release()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return 0, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx,
			`select set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
			return 0, err
		}
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from suite_secrets`).Scan(&n); err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return n, nil
	}

	nA, err := countAs(tA.ID)
	if err != nil {
		t.Fatalf("count as A: %v", err)
	}
	if nA != 1 {
		t.Errorf("tenant A should see 1 secret, got %d", nA)
	}
	nB, err := countAs(tB.ID)
	if err != nil {
		t.Fatalf("count as B: %v", err)
	}
	if nB != 1 {
		t.Errorf("tenant B should see 1 secret, got %d", nB)
	}

	// Read with NO tenant context set (simulating a forgotten resolver).
	// RLS should hide everything — current_setting returns '' and the
	// `nullif(...)::uuid = tenant_id` predicate is FALSE for every row.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	var nNone int
	if err := conn.QueryRow(ctx, `select count(*) from suite_secrets`).Scan(&nNone); err != nil {
		conn.Release()
		t.Fatalf("count no-ctx: %v", err)
	}
	conn.Release()
	if nNone != 0 {
		t.Errorf("with no tenant context RLS should hide all rows, got %d", nNone)
	}

	// Bypass: an admin query opts in and sees both rows.
	conn2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn2.Release()
	tx, err := conn2.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := SetBypassRLS(ctx, conn2, true); err != nil {
		t.Fatalf("set bypass: %v", err)
	}
	var nAll int
	if err := tx.QueryRow(ctx, `select count(*) from suite_secrets`).Scan(&nAll); err != nil {
		t.Fatalf("count all: %v", err)
	}
	_ = tx.Rollback(ctx)
	if nAll != 2 {
		t.Errorf("with bypass admin should see both rows, got %d", nAll)
	}
}

// TestAPIKeyBcryptRoundTrip exercises issue -> verify -> revoke flow.
// Critical safety check: the returned IssuedAPIKey.Value verifies; a
// revoked key fails with ErrKeyRevoked (the resolver maps this to 401
// without leaking the revocation reason on the wire).
func TestAPIKeyBcryptRoundTrip(t *testing.T) {
	mgr, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tA, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "a", Name: "A"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	issued, err := mgr.IssueKey(ctx, IssueAPIKeyInput{
		TenantID: tA.ID,
		Name:     "ci-key",
		Scopes:   []string{"runs:read"},
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Value == "" {
		t.Fatal("expected plaintext value in IssuedAPIKey")
	}
	if !strings.HasPrefix(issued.Value, "af_") {
		t.Errorf("token should start with af_, got %q", issued.Value)
	}
	if issued.Prefix == "" || strings.Contains(issued.Prefix, "_") {
		t.Errorf("prefix should be non-empty and free of separators: %q", issued.Prefix)
	}

	// Verify happy path.
	loaded, err := mgr.VerifyKey(ctx, issued.Value)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if loaded.ID != issued.ID {
		t.Errorf("verify returned id %q, want %q", loaded.ID, issued.ID)
	}
	if loaded.TenantID != tA.ID {
		t.Errorf("verify returned tenant %q, want %q", loaded.TenantID, tA.ID)
	}

	// Tampered secret -> ErrKeySecretMismatch (not ErrAPIKeyNotFound
	// — the prefix still matches).
	tampered := issued.Value[:len(issued.Value)-1] + "x"
	_, err = mgr.VerifyKey(ctx, tampered)
	if !errors.Is(err, ErrKeySecretMismatch) {
		t.Errorf("expected ErrKeySecretMismatch, got %v", err)
	}

	// Unknown prefix -> ErrAPIKeyNotFound (wraps ErrNotFound).
	_, err = mgr.VerifyKey(ctx, "af_aaaaaaaaaaaaaa_"+strings.Repeat("z", 48))
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound, got %v", err)
	}

	// Bad format -> ErrInvalidAPIKeyFormat.
	for _, bad := range []string{"", "not-a-token", "af_", "af__secret", "af_prefix_"} {
		_, err := mgr.VerifyKey(ctx, bad)
		if !errors.Is(err, ErrInvalidAPIKeyFormat) {
			t.Errorf("VerifyKey(%q) = %v, want ErrInvalidAPIKeyFormat", bad, err)
		}
	}

	// Revoke -> ErrKeyRevoked from VerifyKey.
	if err := mgr.RevokeKey(ctx, issued.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err = mgr.VerifyKey(ctx, issued.Value)
	if !errors.Is(err, ErrKeyRevoked) {
		t.Errorf("expected ErrKeyRevoked after revoke, got %v", err)
	}

	// Double-revoke -> ErrAPIKeyNotFound (collapsed: already revoked
	// is indistinguishable from never-existed on the admin surface).
	err = mgr.RevokeKey(ctx, issued.ID)
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Errorf("double revoke should return ErrAPIKeyNotFound, got %v", err)
	}
}

// TestExpiredKeyRejected mints a key with an already-passed expiry and
// verifies VerifyKey returns ErrKeyExpired.
func TestExpiredKeyRejected(t *testing.T) {
	mgr, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tA, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "a", Name: "A"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	issued, err := mgr.IssueKey(ctx, IssueAPIKeyInput{
		TenantID:  tA.ID,
		Scopes:    []string{},
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_, err = mgr.VerifyKey(ctx, issued.Value)
	if !errors.Is(err, ErrKeyExpired) {
		t.Errorf("expected ErrKeyExpired, got %v", err)
	}
}

// TestMembershipsAndUsers exercises AddMembership / ListMemberships /
// RemoveMembership / FirstMembershipFor. The resolver depends on
// FirstMembershipFor working — without a membership row, session-based
// resolution returns ErrMembershipNotFound and the request 403s.
func TestMembershipsAndUsers(t *testing.T) {
	mgr, pool, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	tA, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "a", Name: "A"})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Seed a user manually (no User CRUD on Manager; admin handlers
	// rely on better-auth signup mirroring users in).
	var userID string
	if err := pool.QueryRow(ctx, `
		insert into suite_users (email, name) values ($1, $2)
		returning id::text
	`, "alice@example.com", "Alice").Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// AddMembership happy path + invalid role.
	mem, err := mgr.AddMembership(ctx, tA.ID, userID, "admin")
	if err != nil {
		t.Fatalf("add membership: %v", err)
	}
	if mem.Role != "admin" {
		t.Errorf("role = %q, want admin", mem.Role)
	}
	_, err = mgr.AddMembership(ctx, tA.ID, userID, "godmode")
	if !errors.Is(err, ErrInvalidRole) {
		t.Errorf("expected ErrInvalidRole, got %v", err)
	}

	// FirstMembershipFor must round-trip the tenant id.
	first, err := mgr.FirstMembershipFor(ctx, userID)
	if err != nil {
		t.Fatalf("first membership: %v", err)
	}
	if first.TenantID != tA.ID {
		t.Errorf("first.TenantID = %q, want %q", first.TenantID, tA.ID)
	}

	// ListMembershipsWith filters.
	mems, err := mgr.ListMembershipsWith(ctx, ListMembershipsOpts{TenantID: tA.ID})
	if err != nil {
		t.Fatalf("list memberships: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("expected 1 membership, got %d", len(mems))
	}

	// ListUsersWith with search filter on email substring.
	users, err := mgr.ListUsersWith(ctx, ListUsersOpts{Search: "alice"})
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 || users[0].Email != "alice@example.com" {
		t.Errorf("search miss, got %+v", users)
	}

	// RemoveMembership.
	if err := mgr.RemoveMembership(ctx, tA.ID, userID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := mgr.RemoveMembership(ctx, tA.ID, userID); !errors.Is(err, ErrMembershipNotFound) {
		t.Errorf("second remove should return ErrMembershipNotFound, got %v", err)
	}

	// After removal FirstMembershipFor should return ErrMembershipNotFound.
	_, err = mgr.FirstMembershipFor(ctx, userID)
	if !errors.Is(err, ErrMembershipNotFound) {
		t.Errorf("expected ErrMembershipNotFound after remove, got %v", err)
	}
}

// TestTenantUpdateAndSoftDelete covers UpdateTenant patch semantics and
// the soft-delete contract (deleted_at set, row hidden from ListTenants
// by default, visible with IncludeDeleted).
func TestTenantUpdateAndSoftDelete(t *testing.T) {
	mgr, _, cleanup := testSetup(t)
	defer cleanup()
	ctx := context.Background()

	t1, err := mgr.CreateTenant(ctx, CreateTenantInput{Slug: "co", Name: "Co"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	newName := "Co Renamed"
	updated, err := mgr.UpdateTenant(ctx, t1.ID, UpdateTenantInput{
		Name:     &newName,
		Settings: map[string]interface{}{"feature_x": true},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("name not updated: %q", updated.Name)
	}
	if got, _ := updated.Settings["feature_x"].(bool); !got {
		t.Errorf("settings not merged: %+v", updated.Settings)
	}

	// Soft delete and confirm visibility rules.
	if err := mgr.DeleteTenant(ctx, t1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	live, err := mgr.ListTenants(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, lt := range live {
		if lt.ID == t1.ID {
			t.Errorf("soft-deleted tenant must not appear in default list")
		}
	}
	all, err := mgr.ListTenantsWith(ctx, ListTenantsOpts{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("list-all: %v", err)
	}
	found := false
	for _, lt := range all {
		if lt.ID == t1.ID {
			found = true
			if lt.DeletedAt == nil {
				t.Errorf("deleted_at must be populated for soft-deleted row")
			}
		}
	}
	if !found {
		t.Errorf("IncludeDeleted=true must return the soft-deleted row")
	}

	// Re-delete returns ErrTenantNotFound (already deleted).
	if err := mgr.DeleteTenant(ctx, t1.ID); !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("re-delete should return ErrTenantNotFound, got %v", err)
	}

	// Update on missing id returns ErrTenantNotFound.
	missingName := "ghost"
	_, err = mgr.UpdateTenant(ctx,
		"11111111-1111-1111-1111-111111111111",
		UpdateTenantInput{Name: &missingName})
	if !errors.Is(err, ErrTenantNotFound) {
		t.Errorf("update missing should return ErrTenantNotFound, got %v", err)
	}
}

// TestParseTokenShape is a pure-Go unit test (no DB) that documents the
// `af_<prefix>_<secret>` format. Kept inside the integration suite so
// the parsing rules live next to the verification tests.
func TestParseTokenShape(t *testing.T) {
	cases := []struct {
		in   string
		want bool // true = parses
	}{
		{"", false},
		{"af_", false},
		{"af__secret", false},
		{"af_prefix_", false},
		{"notaf_prefix_secret", false},
		{"af_aaaaaaaaaaaaaaa_" + strings.Repeat("z", 48), true},
	}
	for _, c := range cases {
		_, _, err := parseToken(c.in)
		ok := err == nil
		if ok != c.want {
			t.Errorf("parseToken(%q) ok=%v want %v (err=%v)", c.in, ok, c.want, err)
		}
	}
}