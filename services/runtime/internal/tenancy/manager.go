// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/bcrypt"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
)

// DefaultTenantID is the well-known seed UUID for the "default" tenant
// inserted by migration 00001_init.sql. The resolver falls back to
// this when multi-tenancy is off or there's no resolvable principal.
const DefaultTenantID = "00000000-0000-0000-0000-000000000000"

// keyPrefixBytes / keySecretBytes are the entropy budgets for the two
// halves of a stored API key. The prefix is the lookup index; the
// secret is bcrypt-hashed and is the only proof of possession.
//
// 9 bytes -> 15 base32 chars (no padding). 30 bytes -> 48 base32 chars.
// Combined we get tokens of the shape `af_<15>_<48>` (~70 chars).
const (
	keyPrefixBytes = 9
	keySecretBytes = 30
	// keyTokenPrefix marks AF Stack-issued API keys so callers and
	// log scrubbers can recognise them. Shape: `af_<prefix>_<secret>`.
	keyTokenPrefix = "af_"
)

// bcryptCost is the work factor for hashing API-key secrets. 12 is the
// conventional "human-typed password"-grade choice. API keys are 30
// random bytes (240 bits) so the cost is overkill for entropy, but
// consistency matters more than a few extra ms — and bcrypt's verify
// cost is the same regardless.
const bcryptCost = 12

const tracerName = "af-stack/tenancy"

// validRoles is the set the suite_memberships CHECK constraint
// enforces. Kept here so AddMembership can reject bad input before the
// round-trip.
var validRoles = map[string]struct{}{
	"owner":  {},
	"admin":  {},
	"member": {},
	"viewer": {},
}

// slugRegexp matches the same charset the dashboard's
// CreateTenantInputSchema enforces (lowercase letters, digits, hyphens,
// must not start with hyphen). Length capped at 64 to match the schema.
var slugRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Manager is the concrete, Postgres-backed multi-tenancy manager. One
// instance is constructed at process boot and shared via server.Deps.
//
// Method-name compatibility: the legacy admin.go expects names like
// ListAPIKeys / IssueAPIKey / RevokeAPIKey. The task-spec uses ListKeys /
// IssueKey / RevokeKey. We provide BOTH spellings — the legacy ones
// forward to the canonical implementation so admin.go keeps compiling
// without edits.
type Manager struct {
	pool   *pgxpool.Pool
	hooks  *hooks.Engine
	log    *slog.Logger
	tracer trace.Tracer
	// litellm + secrets are attached via WithLiteLLM at boot. Both
	// are optional — when nil, IssueAPIKey / RevokeAPIKey behave as
	// in pre-#22 builds (no upstream LiteLLM mirror, no per-tenant
	// secret). See litellm_mirror.go.
	litellm LiteLLMMirror
	secrets SecretSink
}

// New constructs a Manager. pool is required; engine and log may be
// nil (we fall back to slog.Default and a no-op hooks engine).
func New(pool *pgxpool.Pool, engine *hooks.Engine, log *slog.Logger) (*Manager, error) {
	if pool == nil {
		return nil, errors.New("tenancy: pool required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		pool:   pool,
		hooks:  engine,
		log:    log,
		tracer: otel.Tracer(tracerName),
	}, nil
}

// Pool exposes the underlying pool. Used by the resolver middleware
// which needs to Acquire a connection to set per-transaction GUCs.
func (m *Manager) Pool() *pgxpool.Pool { return m.pool }

// ─── Tenants ──────────────────────────────────────────────────────────────

// CreateTenant inserts a new tenant row using the structured input.
// Returns ErrSlugTaken on uniqueness violation and ErrInvalidSlug on
// charset/length violation. Fires HookTenantCreated on success.
func (m *Manager) CreateTenant(ctx context.Context, in CreateTenantInput) (Tenant, error) {
	return m.createTenantImpl(ctx, in.Slug, in.Name, in.Plan)
}

// CreateTenantWith is the positional convenience variant matching the
// task spec: (ctx, slug, name, plan). Same semantics as CreateTenant.
func (m *Manager) CreateTenantWith(ctx context.Context, slug, name, plan string) (Tenant, error) {
	return m.createTenantImpl(ctx, slug, name, plan)
}

func (m *Manager) createTenantImpl(ctx context.Context, slug, name, plan string) (Tenant, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.create_tenant",
		trace.WithAttributes(attribute.String("tenancy.slug", slug)),
	)
	defer span.End()

	slug = strings.ToLower(strings.TrimSpace(slug))
	name = strings.TrimSpace(name)
	if !slugRegexp.MatchString(slug) {
		return Tenant{}, ErrInvalidSlug
	}
	if name == "" {
		return Tenant{}, wrappedErr{base: ErrInvalid, msg: "tenancy: name required"}
	}
	if plan == "" {
		plan = "free"
	}

	var (
		id        string
		createdAt time.Time
	)
	err := m.pool.QueryRow(ctx, `
		insert into suite_tenants (slug, name, plan, settings, quota)
		values ($1, $2, $3, '{}'::jsonb, '{}'::jsonb)
		returning id::text, created_at
	`, slug, name, plan).Scan(&id, &createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Tenant{}, ErrSlugTaken
		}
		span.RecordError(err)
		return Tenant{}, fmt.Errorf("tenancy: create tenant: %w", err)
	}

	out := Tenant{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Plan:      plan,
		Settings:  map[string]interface{}{},
		Quota:     map[string]interface{}{},
		CreatedAt: createdAt,
		DeletedAt: nil,
	}

	// Fire hook. Failures are logged but do not roll back the row —
	// hooks are observers, not invariants. Use a fresh background ctx
	// so a request-deadline doesn't abort the hook chain.
	if m.hooks != nil {
		hookCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, hookErr := m.hooks.Fire(hookCtx, hooks.HookTenantCreated, out); hookErr != nil {
			m.log.Warn("tenancy: tenant.created hook returned error",
				"tenant_id", id, "error", hookErr)
		}
		cancel()
	}
	return out, nil
}

// ListTenants returns all non-soft-deleted tenants ordered by
// created_at desc. Matches the existing admin.go call signature.
func (m *Manager) ListTenants(ctx context.Context) ([]Tenant, error) {
	return m.listTenantsImpl(ctx, ListTenantsOpts{})
}

// ListTenantsWith is the structured-opts variant.
func (m *Manager) ListTenantsWith(ctx context.Context, opts ListTenantsOpts) ([]Tenant, error) {
	return m.listTenantsImpl(ctx, opts)
}

func (m *Manager) listTenantsImpl(ctx context.Context, opts ListTenantsOpts) ([]Tenant, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.list_tenants")
	defer span.End()

	q := strings.Builder{}
	q.WriteString(`
		select id::text, slug, name, plan, settings, quota, created_at, deleted_at
		from suite_tenants
	`)
	if !opts.IncludeDeleted {
		q.WriteString(" where deleted_at is null ")
	}
	q.WriteString(" order by created_at desc ")
	if opts.Limit > 0 {
		q.WriteString(fmt.Sprintf(" limit %d ", opts.Limit))
	}
	if opts.Offset > 0 {
		q.WriteString(fmt.Sprintf(" offset %d ", opts.Offset))
	}

	rows, err := m.pool.Query(ctx, q.String())
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("tenancy: list tenants: %w", err)
	}
	defer rows.Close()
	out := []Tenant{}
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTenant returns the tenant detail view (matches existing admin.go
// signature which expects TenantDetail). Internally calls
// GetTenantDetail; the task-spec separates these — see GetTenantOnly
// for the Tenant-only variant.
func (m *Manager) GetTenant(ctx context.Context, id string) (TenantDetail, error) {
	return m.GetTenantDetail(ctx, id)
}

// GetTenantOnly returns just the Tenant row (no joins). Matches the
// task-spec GetTenant(ctx, id) shape.
func (m *Manager) GetTenantOnly(ctx context.Context, id string) (Tenant, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.get_tenant",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", id)),
	)
	defer span.End()

	row := m.pool.QueryRow(ctx, `
		select id::text, slug, name, plan, settings, quota, created_at, deleted_at
		from suite_tenants
		where id = $1
	`, id)
	t, err := scanTenant(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		span.RecordError(err)
		return Tenant{}, fmt.Errorf("tenancy: get tenant: %w", err)
	}
	return t, nil
}

// GetTenantBySlug looks up a tenant by slug. Used by future
// X-AF-Stack-Tenant header resolution.
func (m *Manager) GetTenantBySlug(ctx context.Context, slug string) (Tenant, error) {
	row := m.pool.QueryRow(ctx, `
		select id::text, slug, name, plan, settings, quota, created_at, deleted_at
		from suite_tenants
		where slug = $1
	`, slug)
	t, err := scanTenant(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		return Tenant{}, fmt.Errorf("tenancy: get tenant by slug: %w", err)
	}
	return t, nil
}

// GetTenantDetail returns the tenant plus its members, api keys, and
// rolling usage aggregates. Designed for the dashboard's tenant detail
// page (one logical fetch across several joins).
func (m *Manager) GetTenantDetail(ctx context.Context, id string) (TenantDetail, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.get_tenant_detail",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", id)),
	)
	defer span.End()

	t, err := m.GetTenantOnly(ctx, id)
	if err != nil {
		return TenantDetail{}, err
	}
	members, err := m.tenantMembers(ctx, id)
	if err != nil {
		return TenantDetail{}, err
	}
	keys, err := m.listKeysImpl(ctx, ListKeysOpts{TenantID: id, IncludeRevoked: true})
	if err != nil {
		return TenantDetail{}, err
	}
	usage, err := m.tenantUsage(ctx, id)
	if err != nil {
		return TenantDetail{}, err
	}
	return TenantDetail{
		Tenant:  t,
		Members: members,
		APIKeys: keys,
		Usage:   usage,
	}, nil
}

// tenantMembers loads the {user, role} list for the tenant detail
// page. Joins suite_users so the wire User object is fully populated.
func (m *Manager) tenantMembers(ctx context.Context, tenantID string) ([]TenantDetailMember, error) {
	rows, err := m.pool.Query(ctx, `
		select u.id::text, u.email, u.name, u.avatar_url, u.created_at, u.deleted_at,
		       mem.role
		from suite_memberships mem
		join suite_users u on u.id = mem.user_id
		where mem.tenant_id = $1
		order by mem.invited_at asc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenancy: tenant members: %w", err)
	}
	defer rows.Close()
	out := []TenantDetailMember{}
	for rows.Next() {
		var (
			u    User
			role string
		)
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.DeletedAt, &role); err != nil {
			return nil, err
		}
		out = append(out, TenantDetailMember{User: u, Role: role})
	}
	return out, rows.Err()
}

// tenantUsage aggregates 30-day request count and secrets count.
// Cost is left at 0 (Phase 7 wires the cost ledger). StorageBytes is
// 0 (Phase 5 storage adapter doesn't yet report per-tenant bytes).
// Each sub-query is tolerant of errors — usage is "nice to have" and
// must never block a tenant detail page from rendering.
func (m *Manager) tenantUsage(ctx context.Context, tenantID string) (TenantDetailUsage, error) {
	out := TenantDetailUsage{}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)

	if err := m.pool.QueryRow(ctx, `
		select count(*) from suite_gateway_requests
		where tenant_id = $1 and created_at >= $2
	`, tenantID, cutoff).Scan(&out.Requests30D); err != nil {
		m.log.Warn("tenancy: usage requests_30d failed", "tenant_id", tenantID, "error", err)
	}
	if err := m.pool.QueryRow(ctx, `
		select count(*) from suite_secrets where tenant_id = $1
	`, tenantID).Scan(&out.SecretsCount); err != nil {
		m.log.Warn("tenancy: usage secrets_count failed", "tenant_id", tenantID, "error", err)
	}
	return out, nil
}

// ─── Phase 12.1 — tenant drilldown ────────────────────────────────────────

// GetTenantDrilldown returns the per-tenant aggregate payload backing
// /api/v1/admin/tenants/{id}/drilldown. It's a strict superset of
// GetTenantDetail: members carry last_active_at, usage carries 24-bucket
// sparklines, and the result also includes recent runs, recent webhook
// deliveries, and the optional billing snapshot.
//
// Every sub-query is tolerant — drilldown is a "nice to know everything"
// dashboard view and a failure on any one panel must never collapse the
// whole page. Failures are logged and the corresponding field stays at
// its zero value (empty slice / NULL pointer / zero numeric).
func (m *Manager) GetTenantDrilldown(ctx context.Context, id string) (TenantDrilldown, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.get_tenant_drilldown",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", id)),
	)
	defer span.End()

	t, err := m.GetTenantOnly(ctx, id)
	if err != nil {
		return TenantDrilldown{}, err
	}

	out := TenantDrilldown{
		Tenant:         t,
		Members:        []DrilldownMember{},
		APIKeys:        []APIKey{},
		Usage:          DrilldownUsage{CostSparkline: zeroSparkline24(), RequestSparkline: zeroSparkline24()},
		RecentRuns:     []DrilldownRun{},
		RecentWebhooks: []DrilldownWebhook{},
	}

	if members, err := m.drilldownMembers(ctx, id); err != nil {
		m.log.Warn("tenancy: drilldown members failed", "tenant_id", id, "error", err)
	} else {
		out.Members = members
	}

	if keys, err := m.listKeysImpl(ctx, ListKeysOpts{TenantID: id, IncludeRevoked: true}); err != nil {
		m.log.Warn("tenancy: drilldown keys failed", "tenant_id", id, "error", err)
	} else {
		out.APIKeys = keys
	}

	out.Usage = m.drilldownUsage(ctx, id)

	if runs, err := m.drilldownRecentRuns(ctx, id); err != nil {
		m.log.Warn("tenancy: drilldown runs failed", "tenant_id", id, "error", err)
	} else {
		out.RecentRuns = runs
	}

	if hooks, err := m.drilldownRecentWebhooks(ctx, id); err != nil {
		m.log.Warn("tenancy: drilldown webhooks failed", "tenant_id", id, "error", err)
	} else {
		out.RecentWebhooks = hooks
	}

	if billing, err := m.drilldownBilling(ctx, id); err != nil {
		m.log.Warn("tenancy: drilldown billing failed", "tenant_id", id, "error", err)
	} else {
		out.Billing = billing
	}

	return out, nil
}

// drilldownMembers extends tenantMembers with last_active_at = MAX of
// (suite_api_keys.last_used_at) across the user's keys in this tenant.
// The session table (better-auth) uses TEXT user IDs that don't FK to
// suite_users; joining email-to-email would be brittle, so we keep the
// fallback to NULL and rely on api-key activity.
func (m *Manager) drilldownMembers(ctx context.Context, tenantID string) ([]DrilldownMember, error) {
	rows, err := m.pool.Query(ctx, `
		select u.id::text, u.email, u.name, u.avatar_url, u.created_at, u.deleted_at,
		       mem.role,
		       (
		         select max(k.last_used_at)
		           from suite_api_keys k
		          where k.tenant_id = mem.tenant_id
		            and k.created_by = u.id
		       ) as last_active_at
		  from suite_memberships mem
		  join suite_users u on u.id = mem.user_id
		 where mem.tenant_id = $1
		 order by mem.invited_at asc
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenancy: drilldown members: %w", err)
	}
	defer rows.Close()
	out := []DrilldownMember{}
	for rows.Next() {
		var (
			u            User
			role         string
			lastActiveAt *time.Time
		)
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.DeletedAt, &role, &lastActiveAt); err != nil {
			return nil, err
		}
		out = append(out, DrilldownMember{User: u, Role: role, LastActiveAt: lastActiveAt})
	}
	return out, rows.Err()
}

// drilldownUsage aggregates 30d totals plus 24 hourly buckets for cost
// and requests. Tolerant: any sub-query failure is logged and the field
// stays at zero.
func (m *Manager) drilldownUsage(ctx context.Context, tenantID string) DrilldownUsage {
	out := DrilldownUsage{
		CostSparkline:    zeroSparkline24(),
		RequestSparkline: zeroSparkline24(),
	}
	now := time.Now().UTC()
	cutoff30 := now.Add(-30 * 24 * time.Hour)

	if err := m.pool.QueryRow(ctx, `
		select count(*) from suite_gateway_requests
		 where tenant_id = $1 and created_at >= $2
	`, tenantID, cutoff30).Scan(&out.Requests30D); err != nil {
		m.log.Warn("tenancy: drilldown requests_30d failed", "tenant_id", tenantID, "error", err)
	}

	if err := m.pool.QueryRow(ctx, `
		select coalesce(sum(cost_usd), 0) from suite_cost_events
		 where tenant_id = $1 and occurred_at >= $2
	`, tenantID, cutoff30).Scan(&out.CostUSD30D); err != nil {
		m.log.Warn("tenancy: drilldown cost_usd_30d failed", "tenant_id", tenantID, "error", err)
	}

	if err := m.pool.QueryRow(ctx, `
		select count(*) from suite_secrets where tenant_id = $1
	`, tenantID).Scan(&out.SecretsCount); err != nil {
		m.log.Warn("tenancy: drilldown secrets_count failed", "tenant_id", tenantID, "error", err)
	}

	// StorageBytes left at 0 — the storage adapter doesn't track per-
	// tenant byte counts yet (no suite_storage_objects table).
	out.StorageBytes = 0

	// 24-bucket hourly sparklines for the last 24 hours.
	baseHour := now.Truncate(time.Hour).Add(-23 * time.Hour)
	start := baseHour
	if rows, err := m.pool.Query(ctx, `
		select date_trunc('hour', created_at) as bucket, count(*)
		  from suite_gateway_requests
		 where tenant_id = $1 and created_at >= $2
		 group by bucket
		 order by bucket
	`, tenantID, start); err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				bucket time.Time
				total  int64
			)
			if err := rows.Scan(&bucket, &total); err != nil {
				m.log.Warn("tenancy: drilldown request sparkline scan failed", "tenant_id", tenantID, "error", err)
				break
			}
			idx := int(bucket.Sub(baseHour).Hours())
			if idx >= 0 && idx < 24 {
				out.RequestSparkline[idx] = float64(total)
			}
		}
	} else {
		m.log.Warn("tenancy: drilldown request sparkline failed", "tenant_id", tenantID, "error", err)
	}

	if rows, err := m.pool.Query(ctx, `
		select date_trunc('hour', occurred_at) as bucket, coalesce(sum(cost_usd), 0)
		  from suite_cost_events
		 where tenant_id = $1 and occurred_at >= $2
		 group by bucket
		 order by bucket
	`, tenantID, start); err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				bucket time.Time
				cost   float64
			)
			if err := rows.Scan(&bucket, &cost); err != nil {
				m.log.Warn("tenancy: drilldown cost sparkline scan failed", "tenant_id", tenantID, "error", err)
				break
			}
			idx := int(bucket.Sub(baseHour).Hours())
			if idx >= 0 && idx < 24 {
				out.CostSparkline[idx] = cost
			}
		}
	} else {
		m.log.Warn("tenancy: drilldown cost sparkline failed", "tenant_id", tenantID, "error", err)
	}

	return out
}

// drilldownRecentRuns pulls the 10 most-recent gateway requests that hit
// an agent execute endpoint for this tenant. Joins suite_cost_events on
// the same tenant + tight time window (the same approximation used by
// /api/v1/runs) to approximate the cost of each run.
func (m *Manager) drilldownRecentRuns(ctx context.Context, tenantID string) ([]DrilldownRun, error) {
	rows, err := m.pool.Query(ctx, `
		select r.id::text,
		       r.endpoint,
		       r.status_code,
		       r.duration_ms,
		       r.created_at,
		       coalesce((
		         select sum(e.cost_usd)
		           from suite_cost_events e
		          where e.tenant_id is not distinct from r.tenant_id
		            and e.occurred_at >= r.created_at - interval '5 seconds'
		            and e.occurred_at <= r.created_at + interval '60 seconds'
		       ), 0) as cost_usd
		  from suite_gateway_requests r
		 where r.tenant_id::text = $1
		   and r.endpoint like '/api/v1/execute/%'
		 order by r.created_at desc
		 limit 10
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenancy: drilldown runs: %w", err)
	}
	defer rows.Close()
	out := []DrilldownRun{}
	for rows.Next() {
		var (
			id         string
			endpoint   string
			statusCode *int32
			durationMS *int32
			createdAt  time.Time
			costUSD    float64
		)
		if err := rows.Scan(&id, &endpoint, &statusCode, &durationMS, &createdAt, &costUSD); err != nil {
			return nil, err
		}
		rec := DrilldownRun{
			ID:        id,
			Agent:     agentFromExecuteEndpoint(endpoint),
			Status:    classifyRunStatus(statusCode),
			StartedAt: createdAt.UTC(),
			CostUSD:   costUSD,
		}
		if durationMS != nil {
			rec.DurationMS = int64(*durationMS)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// drilldownRecentWebhooks pulls the 10 most-recent webhook deliveries
// (inbound + outbound) for this tenant.
func (m *Manager) drilldownRecentWebhooks(ctx context.Context, tenantID string) ([]DrilldownWebhook, error) {
	rows, err := m.pool.Query(ctx, `
		select id::text, direction, event_type, status, created_at
		  from suite_webhook_deliveries
		 where tenant_id::text = $1
		 order by created_at desc
		 limit 10
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenancy: drilldown webhooks: %w", err)
	}
	defer rows.Close()
	out := []DrilldownWebhook{}
	for rows.Next() {
		var hk DrilldownWebhook
		if err := rows.Scan(&hk.ID, &hk.Direction, &hk.EventType, &hk.Status, &hk.CreatedAt); err != nil {
			return nil, err
		}
		hk.CreatedAt = hk.CreatedAt.UTC()
		out = append(out, hk)
	}
	return out, rows.Err()
}

// drilldownBilling returns the one suite_billing_customers row for the
// tenant, or nil when the row doesn't exist (treated as "free plan, no
// Stripe wiring yet"). errNoRows is NOT propagated as an error here —
// nil-with-no-error is the correct "no billing snapshot" signal.
func (m *Manager) drilldownBilling(ctx context.Context, tenantID string) (*DrilldownBilling, error) {
	var (
		plan               string
		subscriptionStatus *string
		currentPeriodEnd   *time.Time
		trialEndsAt        *time.Time
	)
	err := m.pool.QueryRow(ctx, `
		select plan, subscription_status, current_period_end, trial_ends_at
		  from suite_billing_customers
		 where tenant_id::text = $1
	`, tenantID).Scan(&plan, &subscriptionStatus, &currentPeriodEnd, &trialEndsAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &DrilldownBilling{
		Plan:               plan,
		SubscriptionStatus: subscriptionStatus,
		CurrentPeriodEnd:   currentPeriodEnd,
		TrialEndsAt:        trialEndsAt,
	}, nil
}

// zeroSparkline24 returns a fresh 24-element zeroed slice. Mirrors the
// dashboard-layer zeroSparkline so we never serialise null for sparkline
// fields (zod expects a 24-length array).
func zeroSparkline24() []float64 {
	return make([]float64, 24)
}

// agentFromExecuteEndpoint extracts the agent name from a gateway-recorded
// endpoint of the form "/api/v1/execute/<agent>" or
// "/api/v1/execute/async/<agent>". Mirrors the dashboard.go helper but is
// duplicated here to avoid an import cycle (server -> tenancy).
func agentFromExecuteEndpoint(endpoint string) string {
	const syncPrefix = "/api/v1/execute/"
	const asyncPrefix = "/api/v1/execute/async/"
	if strings.HasPrefix(endpoint, asyncPrefix) {
		return endpoint[len(asyncPrefix):]
	}
	if strings.HasPrefix(endpoint, syncPrefix) {
		return endpoint[len(syncPrefix):]
	}
	return endpoint
}

// classifyRunStatus maps a gateway status_code to "succeeded" / "failed".
// Mirrors classifyStatus in dashboard.go; duplicated here to avoid a
// server -> tenancy import cycle.
func classifyRunStatus(code *int32) string {
	if code == nil {
		return "failed"
	}
	if *code >= 200 && *code < 300 {
		return "succeeded"
	}
	return "failed"
}

// UpdateTenant patches selected fields. Nil-valued input fields are
// left untouched; passing an empty map for Settings/Quota clears them.
func (m *Manager) UpdateTenant(ctx context.Context, id string, in UpdateTenantInput) (Tenant, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.update_tenant",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", id)),
	)
	defer span.End()

	sets := []string{}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if in.Name != nil {
		sets = append(sets, "name = "+bind(*in.Name))
	}
	if in.Plan != nil {
		sets = append(sets, "plan = "+bind(*in.Plan))
	}
	if in.Settings != nil {
		raw, err := json.Marshal(in.Settings)
		if err != nil {
			return Tenant{}, err
		}
		sets = append(sets, "settings = "+bind(raw)+"::jsonb")
	}
	if in.Quota != nil {
		raw, err := json.Marshal(in.Quota)
		if err != nil {
			return Tenant{}, err
		}
		sets = append(sets, "quota = "+bind(raw)+"::jsonb")
	}
	if len(sets) == 0 {
		return m.GetTenantOnly(ctx, id)
	}
	idBind := bind(id)
	q := fmt.Sprintf(`
		update suite_tenants
		set %s
		where id = %s
		returning id::text, slug, name, plan, settings, quota, created_at, deleted_at
	`, strings.Join(sets, ", "), idBind)

	row := m.pool.QueryRow(ctx, q, args...)
	t, err := scanTenant(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, ErrTenantNotFound
		}
		span.RecordError(err)
		return Tenant{}, fmt.Errorf("tenancy: update tenant: %w", err)
	}
	return t, nil
}

// DeleteTenant soft-deletes by setting deleted_at = now(). Hard delete
// is intentionally not exposed.
func (m *Manager) DeleteTenant(ctx context.Context, id string) error {
	ctx, span := m.tracer.Start(ctx, "tenancy.delete_tenant",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", id)),
	)
	defer span.End()

	tag, err := m.pool.Exec(ctx, `
		update suite_tenants
		set deleted_at = now()
		where id = $1 and deleted_at is null
	`, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("tenancy: delete tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTenantNotFound
	}
	return nil
}

// ─── Users / Memberships ──────────────────────────────────────────────────

// ListUsers matches admin.go's existing signature (ctx, tenantID,
// search). Forwards to ListUsersWith.
func (m *Manager) ListUsers(ctx context.Context, tenantID, search string) ([]User, error) {
	return m.ListUsersWith(ctx, ListUsersOpts{TenantID: tenantID, Search: search})
}

// ListUsersWith is the structured-opts variant matching the task spec.
func (m *Manager) ListUsersWith(ctx context.Context, opts ListUsersOpts) ([]User, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.list_users")
	defer span.End()

	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	conds := []string{"u.deleted_at is null"}
	joins := ""
	if opts.TenantID != "" {
		joins = " join suite_memberships mem on mem.user_id = u.id "
		conds = append(conds, "mem.tenant_id = "+bind(opts.TenantID))
	}
	if s := strings.TrimSpace(opts.Search); s != "" {
		pat := "%" + strings.ToLower(s) + "%"
		conds = append(conds, "(lower(u.email) like "+bind(pat)+" or lower(coalesce(u.name, '')) like "+bind(pat)+")")
	}
	q := "select distinct u.id::text, u.email, u.name, u.avatar_url, u.created_at, u.deleted_at from suite_users u " +
		joins + " where " + strings.Join(conds, " and ") + " order by u.email asc"
	if opts.Limit > 0 {
		q += fmt.Sprintf(" limit %d", opts.Limit)
	}
	if opts.Offset > 0 {
		q += fmt.Sprintf(" offset %d", opts.Offset)
	}

	rows, err := m.pool.Query(ctx, q, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("tenancy: list users: %w", err)
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// AddMembership upserts (tenant, user, role). ErrInvalidRole on bad
// role; FK errors mapped to ErrTenantNotFound or ErrUserNotFound.
func (m *Manager) AddMembership(ctx context.Context, tenantID, userID, role string) (Membership, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.add_membership",
		trace.WithAttributes(
			attribute.String("tenancy.tenant_id", tenantID),
			attribute.String("tenancy.user_id", userID),
			attribute.String("tenancy.role", role),
		),
	)
	defer span.End()

	if _, ok := validRoles[role]; !ok {
		return Membership{}, ErrInvalidRole
	}

	var (
		invitedAt  time.Time
		acceptedAt *time.Time
	)
	err := m.pool.QueryRow(ctx, `
		insert into suite_memberships (tenant_id, user_id, role)
		values ($1, $2, $3)
		on conflict (tenant_id, user_id) do update set role = excluded.role
		returning invited_at, accepted_at
	`, tenantID, userID, role).Scan(&invitedAt, &acceptedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			if !m.tenantExists(ctx, tenantID) {
				return Membership{}, ErrTenantNotFound
			}
			return Membership{}, ErrUserNotFound
		}
		span.RecordError(err)
		return Membership{}, fmt.Errorf("tenancy: add membership: %w", err)
	}
	return Membership{
		TenantID:   tenantID,
		UserID:     userID,
		Role:       role,
		InvitedAt:  invitedAt,
		AcceptedAt: acceptedAt,
	}, nil
}

func (m *Manager) tenantExists(ctx context.Context, id string) bool {
	var exists bool
	err := m.pool.QueryRow(ctx,
		`select exists(select 1 from suite_tenants where id = $1)`, id).Scan(&exists)
	return err == nil && exists
}

// ListMemberships matches admin.go's existing signature
// (ctx, tenantID, userID). Forwards to ListMembershipsWith.
func (m *Manager) ListMemberships(ctx context.Context, tenantID, userID string) ([]Membership, error) {
	return m.ListMembershipsWith(ctx, ListMembershipsOpts{TenantID: tenantID, UserID: userID})
}

// ListMembershipsWith is the structured-opts variant.
func (m *Manager) ListMembershipsWith(ctx context.Context, opts ListMembershipsOpts) ([]Membership, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.list_memberships")
	defer span.End()

	conds := []string{}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.TenantID != "" {
		conds = append(conds, "tenant_id = "+bind(opts.TenantID))
	}
	if opts.UserID != "" {
		conds = append(conds, "user_id = "+bind(opts.UserID))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	q := "select tenant_id::text, user_id::text, role, invited_at, accepted_at from suite_memberships " +
		where + " order by invited_at desc"
	if opts.Limit > 0 {
		q += fmt.Sprintf(" limit %d", opts.Limit)
	}
	if opts.Offset > 0 {
		q += fmt.Sprintf(" offset %d", opts.Offset)
	}

	rows, err := m.pool.Query(ctx, q, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("tenancy: list memberships: %w", err)
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var mem Membership
		if err := rows.Scan(&mem.TenantID, &mem.UserID, &mem.Role, &mem.InvitedAt, &mem.AcceptedAt); err != nil {
			return nil, err
		}
		out = append(out, mem)
	}
	return out, rows.Err()
}

// RemoveMembership deletes a (tenant, user) row.
func (m *Manager) RemoveMembership(ctx context.Context, tenantID, userID string) error {
	ctx, span := m.tracer.Start(ctx, "tenancy.remove_membership",
		trace.WithAttributes(
			attribute.String("tenancy.tenant_id", tenantID),
			attribute.String("tenancy.user_id", userID),
		),
	)
	defer span.End()

	tag, err := m.pool.Exec(ctx, `
		delete from suite_memberships
		where tenant_id = $1 and user_id = $2
	`, tenantID, userID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("tenancy: remove membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

// FirstMembershipFor returns the most-recently-accepted (or earliest-
// invited if none accepted) membership for a user. Used by the
// resolver middleware to pick a tenant when the principal is a session
// rather than an API key.
func (m *Manager) FirstMembershipFor(ctx context.Context, userID string) (Membership, error) {
	row := m.pool.QueryRow(ctx, `
		select tenant_id::text, user_id::text, role, invited_at, accepted_at
		from suite_memberships
		where user_id = $1
		order by accepted_at nulls last, invited_at asc
		limit 1
	`, userID)
	var mem Membership
	if err := row.Scan(&mem.TenantID, &mem.UserID, &mem.Role, &mem.InvitedAt, &mem.AcceptedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Membership{}, ErrMembershipNotFound
		}
		return Membership{}, fmt.Errorf("tenancy: first membership: %w", err)
	}
	return mem, nil
}

// ─── API keys ─────────────────────────────────────────────────────────────

// IssueAPIKey is the legacy admin.go-compatible name; forwards to
// IssueKey.
func (m *Manager) IssueAPIKey(ctx context.Context, in IssueAPIKeyInput) (IssuedAPIKey, error) {
	return m.IssueKey(ctx, in)
}

// IssueKey mints a new api key. Returns IssuedAPIKey whose Value field
// holds the plaintext. Token shape: `af_<prefix15>_<secret48>`. The
// prefix is the unique lookup index; the secret is bcrypt-verified.
//
// The plaintext is shown ONCE — the table only ever stores the bcrypt
// hash of the secret half.
//
// When a LiteLLMMirror is wired (WithLiteLLM) this method ALSO mints a
// LiteLLM virtual key with the supplied budget + rate-limits, stores
// the LiteLLM secret in the secrets vault under
// "litellm/key/{api_key_id}", and updates the suite_api_keys row with
// the alias + hash. Failure of the LiteLLM call rolls back the AF
// Stack row insert so callers see an atomic operation (#22).
//
// If no LiteLLMMirror is wired, the legacy path runs (insert only) and
// the LLM gateway falls back to LITELLM_MASTER_KEY for upstream calls.
// This keeps existing rows + dev setups working unchanged.
func (m *Manager) IssueKey(ctx context.Context, in IssueAPIKeyInput) (IssuedAPIKey, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.issue_key",
		trace.WithAttributes(attribute.String("tenancy.tenant_id", in.TenantID)),
	)
	defer span.End()

	if in.TenantID == "" {
		return IssuedAPIKey{}, wrappedErr{base: ErrInvalid, msg: "tenancy: tenant_id required"}
	}
	if in.Scopes == nil {
		in.Scopes = []string{}
	}

	prefix, err := randomToken(keyPrefixBytes)
	if err != nil {
		return IssuedAPIKey{}, fmt.Errorf("tenancy: prefix entropy: %w", err)
	}
	secret, err := randomToken(keySecretBytes)
	if err != nil {
		return IssuedAPIKey{}, fmt.Errorf("tenancy: secret entropy: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return IssuedAPIKey{}, fmt.Errorf("tenancy: bcrypt: %w", err)
	}

	var (
		id        string
		createdAt time.Time
	)
	var namePtr *string
	if strings.TrimSpace(in.Name) != "" {
		n := in.Name
		namePtr = &n
	}
	var createdByPtr *string
	if strings.TrimSpace(in.CreatedBy) != "" {
		c := in.CreatedBy
		createdByPtr = &c
	}

	err = m.pool.QueryRow(ctx, `
		insert into suite_api_keys
			(tenant_id, prefix, hashed_secret, name, scopes, created_by, expires_at,
			 budget_max_usd, rate_limit_rpm, rate_limit_tpm)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning id::text, created_at
	`,
		in.TenantID, prefix, string(hash), namePtr, in.Scopes, createdByPtr, in.ExpiresAt,
		in.BudgetMaxUSD, in.RateLimitRPM, in.RateLimitTPM,
	).Scan(&id, &createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return IssuedAPIKey{}, ErrTenantNotFound
		}
		span.RecordError(err)
		return IssuedAPIKey{}, fmt.Errorf("tenancy: insert key: %w", err)
	}

	// LiteLLM mirror. Atomic with respect to the row above: if the
	// LiteLLM call fails we delete the row before returning so the
	// caller never sees a half-issued key. The mirror skips silently
	// when LiteLLM isn't wired.
	var (
		litellmAlias string
		litellmHash  string
	)
	if m.litellm != nil && m.litellm.Configured() {
		litellmAlias, litellmHash, err = m.issueLiteLLMKey(ctx, in.TenantID, id, in)
		if err != nil {
			// Roll back. Use a fresh background context so a cancelled
			// request ctx doesn't leave the row stranded — RLS isn't
			// active during the admin path, and the row id is local.
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, delErr := m.pool.Exec(rollbackCtx, `delete from suite_api_keys where id = $1`, id); delErr != nil {
				m.log.Error("tenancy: failed to rollback suite_api_keys row after litellm failure",
					"key_id", id, "error", delErr)
			}
			span.RecordError(err)
			return IssuedAPIKey{}, fmt.Errorf("tenancy: litellm mirror: %w", err)
		}
		// Persist alias + hash on the row. Done in a separate UPDATE
		// rather than the original INSERT because we don't know the
		// alias until LiteLLM responds (the API key id is server-side
		// generated).
		if litellmAlias != "" {
			if _, err := m.pool.Exec(ctx, `
				update suite_api_keys
				set litellm_key_alias = $2, litellm_key_hash = $3
				where id = $1
			`, id, litellmAlias, litellmHash); err != nil {
				m.log.Warn("tenancy: failed to persist litellm alias/hash on row",
					"key_id", id, "alias", litellmAlias, "error", err)
				// Non-fatal — the secret is in the vault under
				// LiteLLMSecretKey(id), so the LLM gateway can still
				// look up by id even without the alias on the row.
				// The cost is that operator inspection of the row
				// won't show the alias.
			}
		}
	}

	pub := APIKey{
		ID:           id,
		TenantID:     in.TenantID,
		Prefix:       prefix,
		Name:         namePtr,
		Scopes:       append([]string{}, in.Scopes...),
		CreatedBy:    createdByPtr,
		CreatedAt:    createdAt,
		ExpiresAt:    in.ExpiresAt,
		BudgetMaxUSD: in.BudgetMaxUSD,
		RateLimitRPM: in.RateLimitRPM,
		RateLimitTPM: in.RateLimitTPM,
	}
	if litellmAlias != "" {
		a := litellmAlias
		pub.LiteLLMKeyAlias = &a
	}
	return IssuedAPIKey{
		APIKey: pub,
		Value:  keyTokenPrefix + prefix + "_" + secret,
	}, nil
}

// ListAPIKeys (legacy admin.go signature) lists keys for a tenant;
// revoked are excluded.
func (m *Manager) ListAPIKeys(ctx context.Context, tenantID string) ([]APIKey, error) {
	return m.listKeysImpl(ctx, ListKeysOpts{TenantID: tenantID, IncludeRevoked: false})
}

// ListKeys (task-spec name) is the structured-opts variant.
func (m *Manager) ListKeys(ctx context.Context, opts ListKeysOpts) ([]APIKey, error) {
	return m.listKeysImpl(ctx, opts)
}

func (m *Manager) listKeysImpl(ctx context.Context, opts ListKeysOpts) ([]APIKey, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.list_keys")
	defer span.End()

	conds := []string{}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.TenantID != "" {
		conds = append(conds, "tenant_id = "+bind(opts.TenantID))
	}
	if !opts.IncludeRevoked {
		conds = append(conds, "revoked_at is null")
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	q := `select id::text, tenant_id::text, prefix, name, scopes, created_by::text,
		         created_at, last_used_at, expires_at, revoked_at,
		         litellm_key_alias, budget_max_usd, rate_limit_rpm, rate_limit_tpm
		  from suite_api_keys ` + where + ` order by created_at desc`
	if opts.Limit > 0 {
		q += fmt.Sprintf(" limit %d", opts.Limit)
	}
	if opts.Offset > 0 {
		q += fmt.Sprintf(" offset %d", opts.Offset)
	}

	rows, err := m.pool.Query(ctx, q, args...)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("tenancy: list keys: %w", err)
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey (legacy admin.go name) forwards to RevokeKey.
func (m *Manager) RevokeAPIKey(ctx context.Context, id string) error {
	return m.RevokeKey(ctx, id)
}

// RevokeKey sets revoked_at = now(). ErrAPIKeyNotFound when no row
// matched OR when already revoked (collapsed into a single not-
// actionable surface).
//
// When a LiteLLMMirror is wired the matching virtual key is deleted
// upstream and the vault entry is cleared. Failures there are logged
// but never propagated — the runtime's auth layer already rejects the
// revoked AF Stack key before the LLM gateway gets a chance to forward
// to LiteLLM, so a stale LiteLLM key is a leak (LiteLLM admin can clean
// up), not a security hole.
func (m *Manager) RevokeKey(ctx context.Context, id string) error {
	ctx, span := m.tracer.Start(ctx, "tenancy.revoke_key",
		trace.WithAttributes(attribute.String("tenancy.api_key_id", id)),
	)
	defer span.End()

	// Pull the alias + tenant id before flipping revoked_at — once
	// revoked, downstream cleanup still needs them.
	var (
		tenantID *string
		alias    *string
	)
	if err := m.pool.QueryRow(ctx, `
		select tenant_id::text, litellm_key_alias
		from suite_api_keys where id = $1
	`, id).Scan(&tenantID, &alias); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAPIKeyNotFound
		}
		span.RecordError(err)
		return fmt.Errorf("tenancy: load key for revoke: %w", err)
	}

	tag, err := m.pool.Exec(ctx, `
		update suite_api_keys
		set revoked_at = now()
		where id = $1 and revoked_at is null
	`, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("tenancy: revoke key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}

	// Best-effort upstream cleanup. Run with a bounded fresh context
	// so a slow LiteLLM doesn't tie up the admin handler past the
	// row update — the row revocation is what matters for security.
	if (alias != nil && *alias != "") || m.secrets != nil {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		go func() {
			defer cancel()
			var tid string
			if tenantID != nil {
				tid = *tenantID
			}
			var al string
			if alias != nil {
				al = *alias
			}
			m.revokeLiteLLMKey(cleanCtx, tid, id, al)
		}()
	}
	return nil
}

// VerifyKey parses an Authorization Bearer value of the form
// `af_<prefix>_<secret>`, looks up the row, and bcrypt-compares the
// secret. On success returns the loaded APIKey for the caller to
// attach to its context.
//
// Error contract (resolver maps all 4xx errors to HTTP 401):
//   - ErrInvalidAPIKeyFormat  — token doesn't parse
//   - ErrAPIKeyNotFound       — prefix lookup found nothing
//   - ErrKeySecretMismatch    — prefix matched but bcrypt failed
//   - ErrKeyRevoked           — matched, but revoked_at is set
//   - ErrKeyExpired           — matched, but expires_at is past
//
// Order matters: we bcrypt-check BEFORE the revoked/expired checks so
// attackers can't tell apart "no such key" from "revoked key" by
// timing or error code.
func (m *Manager) VerifyKey(ctx context.Context, token string) (APIKey, error) {
	prefix, secret, err := parseToken(token)
	if err != nil {
		return APIKey{}, err
	}
	row := m.pool.QueryRow(ctx, `
		select id::text, tenant_id::text, prefix, hashed_secret, name, scopes,
		       created_by::text, created_at, last_used_at, expires_at, revoked_at
		from suite_api_keys
		where prefix = $1
	`, prefix)
	var (
		k      APIKey
		hashed string
	)
	if err := row.Scan(&k.ID, &k.TenantID, &k.Prefix, &hashed, &k.Name, &k.Scopes,
		&k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKey{}, ErrAPIKeyNotFound
		}
		return APIKey{}, fmt.Errorf("tenancy: verify key: %w", err)
	}
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	if bcryptErr := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(secret)); bcryptErr != nil {
		return APIKey{}, ErrKeySecretMismatch
	}
	if k.RevokedAt != nil {
		return k, ErrKeyRevoked
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return k, ErrKeyExpired
	}
	return k, nil
}

// TouchKey best-effort updates last_used_at. Failures are logged and
// swallowed — they should never abort a request.
func (m *Manager) TouchKey(ctx context.Context, id string) {
	_, err := m.pool.Exec(ctx, `
		update suite_api_keys set last_used_at = now() where id = $1
	`, id)
	if err != nil {
		m.log.Warn("tenancy: touch key failed", "key_id", id, "error", err)
	}
}

// ─── Audit ────────────────────────────────────────────────────────────────

// ListAudit returns audit entries scoped by the filter.
func (m *Manager) ListAudit(ctx context.Context, f AuditFilter) (AuditPage, error) {
	ctx, span := m.tracer.Start(ctx, "tenancy.list_audit")
	defer span.End()

	conds := []string{}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.TenantID != "" {
		conds = append(conds, "tenant_id::text = "+bind(f.TenantID))
	}
	if f.Action != "" {
		conds = append(conds, "action = "+bind(f.Action))
	}
	if f.From != nil {
		conds = append(conds, "occurred_at >= "+bind(*f.From))
	}
	if f.To != nil {
		conds = append(conds, "occurred_at <= "+bind(*f.To))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := m.pool.QueryRow(ctx,
		"select count(*) from suite_audit_log"+where, args...).Scan(&total); err != nil {
		span.RecordError(err)
		return AuditPage{}, fmt.Errorf("tenancy: audit count: %w", err)
	}

	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, limit, offset)
	limitIdx := len(pageArgs) - 1
	offsetIdx := len(pageArgs)
	rowsSQL := `select id::text, tenant_id::text, user_id::text, api_key_id::text, action,
		            resource_type, resource_id, metadata, occurred_at
		         from suite_audit_log ` + where +
		" order by occurred_at desc limit $" + fmt.Sprint(limitIdx) +
		" offset $" + fmt.Sprint(offsetIdx)

	rows, err := m.pool.Query(ctx, rowsSQL, pageArgs...)
	if err != nil {
		span.RecordError(err)
		return AuditPage{}, fmt.Errorf("tenancy: audit list: %w", err)
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var (
			e        AuditEntry
			tenantID *string
			userID   *string
			apiKeyID *string
			metaRaw  []byte
		)
		if err := rows.Scan(&e.ID, &tenantID, &userID, &apiKeyID, &e.Action,
			&e.ResourceType, &e.ResourceID, &metaRaw, &e.OccurredAt); err != nil {
			return AuditPage{}, err
		}
		e.TenantID = tenantID
		e.UserID = userID
		e.APIKeyID = apiKeyID
		if len(metaRaw) > 0 {
			if err := json.Unmarshal(metaRaw, &e.Metadata); err != nil {
				return AuditPage{}, fmt.Errorf("tenancy: audit metadata decode: %w", err)
			}
		}
		if e.Metadata == nil {
			e.Metadata = map[string]interface{}{}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return AuditPage{}, err
	}
	return AuditPage{
		Entries: out,
		Total:   total,
		HasMore: offset+len(out) < total,
	}, nil
}

// ─── Resolver helpers ─────────────────────────────────────────────────────

// SetTenantContext binds the runtime PG session to a tenant for RLS.
// Run inside a transaction (set_config with is_local=true scopes the
// binding to the current transaction). Pass empty tenantID to clear.
func SetTenantContext(ctx context.Context, conn *pgxpool.Conn, tenantID string) error {
	if conn == nil {
		return errors.New("tenancy: nil conn")
	}
	_, err := conn.Exec(ctx, `select set_config('app.tenant_id', $1, true)`, tenantID)
	if err != nil {
		return fmt.Errorf("tenancy: set_config tenant: %w", err)
	}
	return nil
}

// SetBypassRLS toggles the admin escape hatch. Called by admin
// handlers that need cross-tenant visibility. Setting auto-clears at
// the end of the transaction (is_local=true).
func SetBypassRLS(ctx context.Context, conn *pgxpool.Conn, on bool) error {
	if conn == nil {
		return errors.New("tenancy: nil conn")
	}
	v := "off"
	if on {
		v = "on"
	}
	_, err := conn.Exec(ctx, `select set_config('app.bypass_rls', $1, true)`, v)
	if err != nil {
		return fmt.Errorf("tenancy: set_config bypass: %w", err)
	}
	return nil
}

// ─── Scanner helpers ──────────────────────────────────────────────────────

// rowScanner abstracts pgx.Row and pgx.Rows so scanTenant works for
// both QueryRow and Query loops.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTenant reads a `select id, slug, name, plan, settings, quota,
// created_at, deleted_at` row.
func scanTenant(r rowScanner) (Tenant, error) {
	var (
		t           Tenant
		settingsRaw []byte
		quotaRaw    []byte
	)
	if err := r.Scan(&t.ID, &t.Slug, &t.Name, &t.Plan, &settingsRaw, &quotaRaw,
		&t.CreatedAt, &t.DeletedAt); err != nil {
		return Tenant{}, err
	}
	if len(settingsRaw) > 0 {
		if err := json.Unmarshal(settingsRaw, &t.Settings); err != nil {
			return Tenant{}, fmt.Errorf("tenancy: decode settings: %w", err)
		}
	}
	if t.Settings == nil {
		t.Settings = map[string]interface{}{}
	}
	if len(quotaRaw) > 0 {
		if err := json.Unmarshal(quotaRaw, &t.Quota); err != nil {
			return Tenant{}, fmt.Errorf("tenancy: decode quota: %w", err)
		}
	}
	if t.Quota == nil {
		t.Quota = map[string]interface{}{}
	}
	return t, nil
}

// scanAPIKey reads a `select id, tenant_id, prefix, name, scopes,
// created_by, created_at, last_used_at, expires_at, revoked_at,
// litellm_key_alias, budget_max_usd, rate_limit_rpm, rate_limit_tpm`
// row. hashed_secret + litellm_key_hash are NEVER projected by callers
// of this helper.
func scanAPIKey(r rowScanner) (APIKey, error) {
	var k APIKey
	if err := r.Scan(&k.ID, &k.TenantID, &k.Prefix, &k.Name, &k.Scopes, &k.CreatedBy,
		&k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt,
		&k.LiteLLMKeyAlias, &k.BudgetMaxUSD, &k.RateLimitRPM, &k.RateLimitTPM); err != nil {
		return APIKey{}, err
	}
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	return k, nil
}

// ─── Token helpers ────────────────────────────────────────────────────────

// randomToken returns a base32-encoded random string with `bytesN`
// bytes of entropy. base32 (lower-case, no padding) keeps the token
// URL-safe and visually distinguishable.
func randomToken(bytesN int) (string, error) {
	buf := make([]byte, bytesN)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return strings.ToLower(encoded), nil
}

// parseToken splits a `af_<prefix>_<secret>` token into its halves.
// Returns ErrInvalidAPIKeyFormat for any shape mismatch.
func parseToken(token string) (prefix, secret string, err error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, keyTokenPrefix) {
		return "", "", ErrInvalidAPIKeyFormat
	}
	rest := strings.TrimPrefix(token, keyTokenPrefix)
	idx := strings.IndexByte(rest, '_')
	if idx <= 0 || idx >= len(rest)-1 {
		return "", "", ErrInvalidAPIKeyFormat
	}
	prefix = rest[:idx]
	secret = rest[idx+1:]
	if prefix == "" || secret == "" {
		return "", "", ErrInvalidAPIKeyFormat
	}
	return prefix, secret, nil
}
