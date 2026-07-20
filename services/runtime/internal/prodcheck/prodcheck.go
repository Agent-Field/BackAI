// SPDX-License-Identifier: Apache-2.0

// Package prodcheck implements the BackAI production operating contract as
// enforceable checks rather than documentation.
//
// The contract is armed when the runtime is in SaaS mode AND
// AF_STACK_ENV=production (see config.Config.ProductionHardening). When armed,
// two call sites consume this package:
//
//   - Startup preflight (cmd/af-stack): a failing check is a fatal boot error
//     so a mis-hardened deployment never accepts traffic.
//   - Readiness (/ready): the same posture is folded into readiness so drift
//     after boot (e.g. an operator ALTERs a table's RLS) turns the pod
//     un-ready instead of silently leaking.
//
// The check *logic* is a set of pure functions over plain inputs so it can be
// unit-tested against fabricated catalog rows and config combinations without a
// live database. Live gathering (role security + tenant-table RLS) is a thin
// layer over a pgx Querier that both call sites share.
package prodcheck

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Status is the outcome of a single check.
type Status string

const (
	// StatusPass — the posture requirement is satisfied.
	StatusPass Status = "pass"
	// StatusFail — the requirement is violated; boot must abort / readiness
	// must go 503.
	StatusFail Status = "fail"
	// StatusSkip — the requirement does not apply (e.g. storage isolation when
	// no storage adapter is configured).
	StatusSkip Status = "skip"
)

// Stable codes. These are part of the operating contract: they appear in fatal
// boot logs and in the /ready envelope, so operators and tooling can branch on
// them. Do not rename without a deprecation.
const (
	CodeDBRoleBypassesRLS     = "PRODCHECK_DB_ROLE_BYPASSES_RLS"
	CodeTenantTableRLSMissing = "PRODCHECK_TENANT_TABLE_RLS_MISSING"
	CodeCORSWildcardCreds     = "PRODCHECK_CORS_WILDCARD_CREDENTIALED"
	CodeSecretsDevKey         = "PRODCHECK_SECRETS_DEV_KEY"
	CodeStorageNotIsolated    = "PRODCHECK_STORAGE_NOT_ISOLATED"
	CodeSandboxNetworkOpen    = "PRODCHECK_SANDBOX_NETWORK_OPEN"
	CodeCatalogUnavailable    = "PRODCHECK_CATALOG_UNAVAILABLE"
)

// Result is one check outcome.
type Result struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// Failed reports whether this result should block boot / readiness.
func (r Result) Failed() bool { return r.Status == StatusFail }

// Report is the aggregate of every check.
type Report struct {
	Results []Result `json:"results"`
}

// OK reports whether every check passed or was skipped (i.e. none failed).
func (rep Report) OK() bool {
	for _, r := range rep.Results {
		if r.Failed() {
			return false
		}
	}
	return true
}

// Failures returns just the failing results, in evaluation order.
func (rep Report) Failures() []Result {
	var out []Result
	for _, r := range rep.Results {
		if r.Failed() {
			out = append(out, r)
		}
	}
	return out
}

// FirstFailureCode returns the code of the first failing check, or "" when the
// report is clean. Used by /ready to surface a single machine-readable code.
func (rep Report) FirstFailureCode() string {
	for _, r := range rep.Results {
		if r.Failed() {
			return r.Code
		}
	}
	return ""
}

// TableRLS describes one public base table's row-level-security posture.
type TableRLS struct {
	Table       string
	HasTenantID bool
	RLSEnabled  bool
	RLSForced   bool
}

// Inputs is the complete set of facts the checks evaluate. The DB-derived
// fields (DBRole*, Tables) come from Gather; the rest are config-derived and
// supplied by the caller.
type Inputs struct {
	DBRoleName  string
	DBSuperuser bool
	DBBypassRLS bool
	Tables      []TableRLS

	CORSOrigins          []string
	KMSConfigured        bool
	KMSDevMode           bool
	StorageConfigured    bool
	MultiTenancyEnabled  bool
	SandboxNetworkPolicy string
}

// Evaluate runs every check against the given inputs and returns the report.
// The order is stable (most-fundamental isolation guarantees first).
func Evaluate(in Inputs) Report {
	return Report{Results: []Result{
		CheckDBRole(in.DBRoleName, in.DBSuperuser, in.DBBypassRLS),
		CheckTenantRLS(in.Tables),
		CheckSecretsKMS(in.KMSConfigured, in.KMSDevMode),
		CheckCORS(in.CORSOrigins),
		CheckStorageIsolation(in.StorageConfigured, in.MultiTenancyEnabled),
		CheckSandboxNetwork(in.SandboxNetworkPolicy),
	}}
}

// CheckDBRole fails when the serving role can bypass row-level security — a
// superuser or a BYPASSRLS role makes per-tenant isolation unenforceable.
func CheckDBRole(role string, superuser, bypassRLS bool) Result {
	if superuser || bypassRLS {
		return Result{
			Name:   "db serving role RLS-safe",
			Code:   CodeDBRoleBypassesRLS,
			Status: StatusFail,
			Detail: fmt.Sprintf("serving role %q bypasses RLS (superuser=%v bypassrls=%v)", role, superuser, bypassRLS),
			Fix:    "point AF_STACK_DATABASE_URL at a NOSUPERUSER NOBYPASSRLS role and run migrations via AF_STACK_MIGRATE_DATABASE_URL",
		}
	}
	return Result{Name: "db serving role RLS-safe", Code: CodeDBRoleBypassesRLS, Status: StatusPass}
}

// CheckTenantRLS fails when any tenant-owned table (a public base table with a
// tenant_id column) does not have row-level security both ENABLED and FORCED.
// FORCE is required because the table owner would otherwise silently skip the
// policy. Tables without a tenant_id column are ignored — they are not
// tenant-scoped.
func CheckTenantRLS(tables []TableRLS) Result {
	var offenders []string
	tenantTables := 0
	for _, t := range tables {
		if !t.HasTenantID {
			continue
		}
		tenantTables++
		if !t.RLSEnabled || !t.RLSForced {
			offenders = append(offenders, fmt.Sprintf("%s(enabled=%v forced=%v)", t.Table, t.RLSEnabled, t.RLSForced))
		}
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return Result{
			Name:   "tenant tables RLS enabled+forced",
			Code:   CodeTenantTableRLSMissing,
			Status: StatusFail,
			Detail: fmt.Sprintf("%d tenant-owned table(s) missing enable+force RLS: %s", len(offenders), strings.Join(offenders, ", ")),
			Fix:    "add `alter table <t> enable row level security; alter table <t> force row level security;` plus a tenant_isolation policy (see migration 00032_memory_tenant_rls.sql)",
		}
	}
	return Result{
		Name:   "tenant tables RLS enabled+forced",
		Code:   CodeTenantTableRLSMissing,
		Status: StatusPass,
		Detail: fmt.Sprintf("%d tenant-owned table(s) verified", tenantTables),
	}
}

// CheckSecretsKMS fails when a real KMS/master key is not configured, or when
// the loaded cipher is the dev sentinel. In production the vault (secrets,
// OAuth tokens, webhook signing) must be encrypted with a real key.
func CheckSecretsKMS(kmsConfigured, devMode bool) Result {
	if !kmsConfigured || devMode {
		return Result{
			Name:   "secrets KMS key configured",
			Code:   CodeSecretsDevKey,
			Status: StatusFail,
			Detail: fmt.Sprintf("KMS key not production-configured (configured=%v dev_mode=%v)", kmsConfigured, devMode),
			Fix:    "set AF_STACK_KMS_KEY to 32 random bytes hex-encoded, or configure a cloud provider via AF_STACK_KMS_PROVIDER",
		}
	}
	return Result{Name: "secrets KMS key configured", Code: CodeSecretsDevKey, Status: StatusPass}
}

// CheckCORS fails when the credentialed-origin allowlist contains a wildcard.
// A "*" origin combined with Access-Control-Allow-Credentials would let any
// site drive authenticated cross-origin requests — the exact CSRF footgun the
// allowlist exists to prevent.
func CheckCORS(origins []string) Result {
	for _, o := range origins {
		if strings.TrimSpace(o) == "*" {
			return Result{
				Name:   "no wildcard credentialed CORS",
				Code:   CodeCORSWildcardCreds,
				Status: StatusFail,
				Detail: "AF_STACK_CORS_ORIGINS contains a '*' wildcard; credentialed cross-origin requests would be unrestricted",
				Fix:    "list explicit https origins in AF_STACK_CORS_ORIGINS instead of '*'",
			}
		}
	}
	return Result{Name: "no wildcard credentialed CORS", Code: CodeCORSWildcardCreds, Status: StatusPass}
}

// CheckStorageIsolation fails when object storage is configured but
// multi-tenancy is off — without multi-tenancy the per-tenant object-key
// prefix isolation (the S8 fix) does not engage and tenants share one
// namespace. Skips when no storage adapter is wired.
func CheckStorageIsolation(storageConfigured, multiTenancyEnabled bool) Result {
	if !storageConfigured {
		return Result{
			Name:   "object storage tenant-isolated",
			Code:   CodeStorageNotIsolated,
			Status: StatusSkip,
			Detail: "no object-storage adapter configured",
		}
	}
	if !multiTenancyEnabled {
		return Result{
			Name:   "object storage tenant-isolated",
			Code:   CodeStorageNotIsolated,
			Status: StatusFail,
			Detail: "object storage is configured but multi-tenancy is disabled; per-tenant key isolation is not enforced",
			Fix:    "enable the multi-tenancy module (AF_STACK_MODULE_MULTI_TENANCY=true) so object keys are prefixed per tenant",
		}
	}
	return Result{Name: "object storage tenant-isolated", Code: CodeStorageNotIsolated, Status: StatusPass}
}

// CheckSandboxNetwork fails when the default sandbox network policy is anything
// other than "isolated". The open bridge can reach host-published services
// (including the suite Postgres), so it is a cross-tenant escape hatch that
// must not be the production default.
func CheckSandboxNetwork(policy string) Result {
	p := strings.ToLower(strings.TrimSpace(policy))
	if p == "" || p == "isolated" {
		return Result{Name: "sandbox network policy isolated", Code: CodeSandboxNetworkOpen, Status: StatusPass}
	}
	return Result{
		Name:   "sandbox network policy isolated",
		Code:   CodeSandboxNetworkOpen,
		Status: StatusFail,
		Detail: fmt.Sprintf("default sandbox network policy is %q, not isolated", p),
		Fix:    "set AF_STACK_SANDBOX_NETWORK_POLICY=isolated (or unset it) for production",
	}
}

// Querier is the minimal pgx surface the live gatherers need. Both
// *pgxpool.Pool and *pgx.Conn satisfy it.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ConfigInputs are the non-DB posture facts the caller derives from config.
type ConfigInputs struct {
	CORSOrigins          []string
	KMSConfigured        bool
	KMSDevMode           bool
	StorageConfigured    bool
	MultiTenancyEnabled  bool
	SandboxNetworkPolicy string
}

const roleSecurityQuery = `select rolname, rolsuper, rolbypassrls from pg_roles where rolname = current_user`

// tenantTableRLSQuery lists every public base table with its RLS flags and
// whether it carries a tenant_id column. pg_attribute is used (not
// information_schema) so the answer reflects the raw catalog for the
// connecting role.
const tenantTableRLSQuery = `
select c.relname,
       c.relrowsecurity,
       c.relforcerowsecurity,
       exists (
         select 1 from pg_attribute a
          where a.attrelid = c.oid
            and a.attname = 'tenant_id'
            and a.attnum > 0
            and not a.attisdropped
       ) as has_tenant_id
  from pg_class c
  join pg_namespace n on n.oid = c.relnamespace
 where n.nspname = 'public'
   and c.relkind = 'r'
 order by c.relname`

// GatherRoleSecurity reads the connecting role's RLS-relevant attributes.
func GatherRoleSecurity(ctx context.Context, q Querier) (name string, superuser, bypassRLS bool, err error) {
	err = q.QueryRow(ctx, roleSecurityQuery).Scan(&name, &superuser, &bypassRLS)
	if err != nil {
		return "", false, false, fmt.Errorf("prodcheck: read role security: %w", err)
	}
	return name, superuser, bypassRLS, nil
}

// GatherTableRLS reads the RLS posture of every public base table.
func GatherTableRLS(ctx context.Context, q Querier) ([]TableRLS, error) {
	rows, err := q.Query(ctx, tenantTableRLSQuery)
	if err != nil {
		return nil, fmt.Errorf("prodcheck: query tenant table rls: %w", err)
	}
	defer rows.Close()
	var out []TableRLS
	for rows.Next() {
		var t TableRLS
		if err := rows.Scan(&t.Table, &t.RLSEnabled, &t.RLSForced, &t.HasTenantID); err != nil {
			return nil, fmt.Errorf("prodcheck: scan tenant table rls: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("prodcheck: iterate tenant table rls: %w", err)
	}
	return out, nil
}

// Gather assembles full Inputs by combining caller-supplied config facts with a
// live catalog query (role security + tenant-table RLS). A catalog error is
// returned so the caller can decide fatal-vs-degraded; the returned Inputs
// still carry the config facts so a partial evaluation is possible.
func Gather(ctx context.Context, q Querier, cfg ConfigInputs) (Inputs, error) {
	in := Inputs{
		CORSOrigins:          cfg.CORSOrigins,
		KMSConfigured:        cfg.KMSConfigured,
		KMSDevMode:           cfg.KMSDevMode,
		StorageConfigured:    cfg.StorageConfigured,
		MultiTenancyEnabled:  cfg.MultiTenancyEnabled,
		SandboxNetworkPolicy: cfg.SandboxNetworkPolicy,
	}
	if q == nil {
		return in, fmt.Errorf("prodcheck: nil querier")
	}
	name, super, bypass, err := GatherRoleSecurity(ctx, q)
	if err != nil {
		return in, err
	}
	in.DBRoleName, in.DBSuperuser, in.DBBypassRLS = name, super, bypass
	tables, err := GatherTableRLS(ctx, q)
	if err != nil {
		return in, err
	}
	in.Tables = tables
	return in, nil
}
