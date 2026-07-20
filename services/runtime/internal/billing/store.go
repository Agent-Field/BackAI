// SPDX-License-Identifier: Apache-2.0

// store.go — persistence for suite_billing_customers + suite_usage_meters.
//
// The Store is constructed once per process and shared by:
//   - the Service (per-tenant verbs: meter, has_budget, portal_link),
//   - the webhook handler (upserts customer rows from Stripe events),
//   - the REST surface (cross-tenant listings under app.bypass_rls).
//
// A nil pool means "no DB" — every read returns empty / not-found and
// every write returns ErrNotFound (writes to nil-pool aren't meaningful;
// callers should branch on Store.HasPool() before invoking).
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Store wraps the SQL surface for billing tables.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewStore constructs a Store. pool may be nil; log defaults to
// slog.Default().
func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log}
}

// HasPool reports whether the Store can talk to a database.
func (s *Store) HasPool() bool {
	return s != nil && s.pool != nil
}

// ─── Customers ────────────────────────────────────────────────────────────

// ListCustomers returns every billing customer row. Used by the
// dashboard's billing tab to show one row per tenant. The caller is
// responsible for setting app.bypass_rls=on transactionally; without it
// the RLS policy will silently filter to the current tenant.
func (s *Store) ListCustomers(ctx context.Context) ([]Customer, error) {
	if !s.HasPool() {
		return []Customer{}, nil
	}
	const q = `
		select tenant_id::text, stripe_customer_id, email, plan,
		       trial_ends_at, current_period_end, subscription_status,
		       created_at, updated_at
		  from suite_billing_customers
		 order by created_at desc
	`
	// Operator cross-tenant listing: no tenant to bind, so read inside a
	// bypass_rls transaction (same pattern as ListMeters). Without it the
	// FORCE-RLS policy on suite_billing_customers hides every row and the
	// dashboard customers table is always empty.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: list customers begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return nil, fmt.Errorf("billing: list customers bypass: %w", err)
	}
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("billing: list customers: %w", err)
	}
	defer rows.Close()

	out := make([]Customer, 0)
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("billing: iterate customers: %w", err)
	}
	return out, nil
}

// GetCustomer returns the billing row for tenantID. Returns ErrNotFound
// when no row exists.
func (s *Store) GetCustomer(ctx context.Context, tenantID string) (Customer, error) {
	if !s.HasPool() {
		return Customer{}, ErrNotFound
	}
	if strings.TrimSpace(tenantID) == "" {
		return Customer{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	// RLS: bind the tenant or the force policy hides the row (this runs on
	// the operator/dashboard path where app.tenant_id is empty), so a
	// tenant that upgraded keeps reading back as "free". Same fix as
	// GetTotalForPeriod.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	const q = `
		select tenant_id::text, stripe_customer_id, email, plan,
		       trial_ends_at, current_period_end, subscription_status,
		       created_at, updated_at
		  from suite_billing_customers
		 where tenant_id = $1
	`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return Customer{}, fmt.Errorf("billing: get customer: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Customer{}, err
		}
		return Customer{}, ErrNotFound
	}
	return scanCustomer(rows)
}

// UpsertCustomer creates or updates the billing row for the tenant.
// Empty pointer fields are written as NULL; the plan defaults to "free"
// on insert when not supplied. updated_at always refreshes.
//
// This is the write path used by the webhook handler (Stripe pushes
// customer.subscription.updated, the handler maps it to this call).
func (s *Store) UpsertCustomer(ctx context.Context, c Customer) (Customer, error) {
	if !s.HasPool() {
		return Customer{}, ErrNotFound
	}
	if strings.TrimSpace(c.TenantID) == "" {
		return Customer{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	// Bind tenant so the RLS force policy permits the upsert on
	// suite_billing_customers. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, c.TenantID, "")
	// An empty plan means "don't change the plan": default it to free on a
	// fresh insert, but PRESERVE the existing plan on update. Callers that
	// only touch subscription_status / current_period_end (e.g. the
	// invoice.payment_succeeded and dunning webhook paths) pass "" here — the
	// SQL below coalesces so those events never clobber a paid plan back to
	// free. An explicit plan (checkout, subscription.updated) still wins.
	plan := strings.TrimSpace(c.Plan)

	var (
		trialEndsPtr, periodEndPtr *time.Time
	)
	if c.TrialEndsAt != nil && *c.TrialEndsAt != "" {
		if t, err := parseRFC3339(*c.TrialEndsAt); err == nil {
			trialEndsPtr = &t
		}
	}
	if c.CurrentPeriodEnd != nil && *c.CurrentPeriodEnd != "" {
		if t, err := parseRFC3339(*c.CurrentPeriodEnd); err == nil {
			periodEndPtr = &t
		}
	}

	const q = `
		insert into suite_billing_customers
			(tenant_id, stripe_customer_id, email, plan,
			 trial_ends_at, current_period_end, subscription_status)
		values ($1, $2, $3, coalesce(nullif($4, ''), 'free'), $5, $6, $7)
		on conflict (tenant_id) do update set
			stripe_customer_id  = coalesce(excluded.stripe_customer_id,  suite_billing_customers.stripe_customer_id),
			email               = coalesce(excluded.email,               suite_billing_customers.email),
			plan                = coalesce(nullif($4, ''),               suite_billing_customers.plan),
			trial_ends_at       = coalesce(excluded.trial_ends_at,       suite_billing_customers.trial_ends_at),
			current_period_end  = coalesce(excluded.current_period_end,  suite_billing_customers.current_period_end),
			subscription_status = coalesce(excluded.subscription_status, suite_billing_customers.subscription_status),
			updated_at          = now()
		returning tenant_id::text, stripe_customer_id, email, plan,
		          trial_ends_at, current_period_end, subscription_status,
		          created_at, updated_at
	`
	rows, err := s.pool.Query(ctx, q,
		c.TenantID, c.StripeCustomerID, c.Email, plan,
		trialEndsPtr, periodEndPtr, c.SubscriptionStatus,
	)
	if err != nil {
		return Customer{}, fmt.Errorf("billing: upsert customer: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Customer{}, err
		}
		return Customer{}, ErrNotFound
	}
	return scanCustomer(rows)
}

// RecordBillingEvent records an ingested billing event id for idempotency
// (webhook + reconciliation). Returns firstTime=true when this
// (tenant, event_id) pair was newly inserted, false when it was already
// present — a replay the caller should skip re-applying. Binds the tenant so
// the FORCE-RLS WITH CHECK on suite_billing_events passes.
//
// Fails open: with no pool, or an empty tenant/event id, it returns
// firstTime=true so ingestion is never blocked by a missing idempotency
// ledger (correctness of the applied state is idempotent anyway).
func (s *Store) RecordBillingEvent(ctx context.Context, tenantID, eventID, eventType string) (bool, error) {
	if !s.HasPool() {
		return true, nil
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(eventID) == "" {
		return true, nil
	}
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	tag, err := s.pool.Exec(ctx, `
		insert into suite_billing_events (tenant_id, event_id, event_type)
		values ($1, $2, $3)
		on conflict (tenant_id, event_id) do nothing
	`, tenantID, eventID, eventType)
	if err != nil {
		return false, fmt.Errorf("billing: record event: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ─── Meters ───────────────────────────────────────────────────────────────

// MeterFilters scopes a ListMeters call. Empty fields mean "no filter".
type MeterFilters struct {
	TenantID    string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// ListMeters returns rows from suite_usage_meters matching filters.
//
// When TenantID is empty we fall back to listing across all tenants —
// the caller (REST handler) is responsible for setting app.bypass_rls=on
// in that case. Period bounds are inclusive-exclusive [start, end).
func (s *Store) ListMeters(ctx context.Context, f MeterFilters) ([]UsageMeter, error) {
	if !s.HasPool() {
		return []UsageMeter{}, nil
	}
	// RLS: writes bind the tenant (IncrementMeter), so reads must too or
	// the force policy filters every row and the endpoint reports zero
	// usage forever. Tenant-scoped reads bind that tenant; the operator's
	// cross-tenant listing (no tenant filter) reads via bypass_rls below,
	// mirroring webhooks/deliveries.go.
	if f.TenantID != "" {
		ctx = tenantctx.WithTenant(ctx, f.TenantID, "")
	}
	conds := []string{}
	args := []any{}
	if f.TenantID != "" {
		args = append(args, f.TenantID)
		conds = append(conds, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if !f.PeriodStart.IsZero() {
		args = append(args, f.PeriodStart.UTC())
		conds = append(conds, fmt.Sprintf("period_start >= $%d", len(args)))
	}
	if !f.PeriodEnd.IsZero() {
		args = append(args, f.PeriodEnd.UTC())
		conds = append(conds, fmt.Sprintf("period_start < $%d", len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " where " + strings.Join(conds, " and ")
	}
	q := `
		select meter, tenant_id::text, period_start, period_end,
		       quantity::float8, cost_usd, stripe_meter_id, last_synced_at
		  from suite_usage_meters
	` + where + ` order by period_start desc, meter asc`

	scan := func(rows pgx.Rows) ([]UsageMeter, error) {
		defer rows.Close()
		out := make([]UsageMeter, 0)
		for rows.Next() {
			m, err := scanMeter(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("billing: iterate meters: %w", err)
		}
		return out, nil
	}

	if f.TenantID != "" {
		rows, err := s.pool.Query(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("billing: list meters: %w", err)
		}
		return scan(rows)
	}

	// Operator cross-tenant listing: no tenant to bind, so read inside a
	// bypass_rls transaction (same pattern as the deliveries store).
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing: list meters begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		return nil, fmt.Errorf("billing: list meters bypass: %w", err)
	}
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("billing: list meters: %w", err)
	}
	out, err := scan(rows)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("billing: list meters commit: %w", err)
	}
	return out, nil
}

// IncrementMeter additively bumps the (meter, tenant, period) row's
// quantity by qty and, when priceUSD > 0, cost_usd by qty*priceUSD.
// Creates the row when absent.
//
// This is the hot path called from sandbox/LLM/etc. — keep it cheap.
func (s *Store) IncrementMeter(
	ctx context.Context,
	meter, tenantID string,
	periodStart, periodEnd time.Time,
	qty, priceUSD float64,
) error {
	if !s.HasPool() {
		return nil
	}
	if strings.TrimSpace(meter) == "" || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: meter and tenant_id are required", ErrInvalidInput)
	}
	// Bind tenant so the RLS force policy permits the upsert on
	// suite_usage_meters. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	const q = `
		insert into suite_usage_meters
			(meter, tenant_id, period_start, period_end, quantity, cost_usd)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (meter, tenant_id, period_start) do update set
			quantity = suite_usage_meters.quantity + excluded.quantity,
			cost_usd = case
			    when suite_usage_meters.cost_usd is null and excluded.cost_usd is null then null
			    else coalesce(suite_usage_meters.cost_usd, 0) + coalesce(excluded.cost_usd, 0)
			  end
	`
	var costArg any
	if priceUSD > 0 {
		costArg = qty * priceUSD
	}
	if _, err := s.pool.Exec(ctx, q,
		meter, tenantID, periodStart.UTC(), periodEnd.UTC(), qty, costArg,
	); err != nil {
		return fmt.Errorf("billing: increment meter: %w", err)
	}
	return nil
}

// GetTotalForPeriod returns the total cost_usd across all meters for the
// given tenant + period window. Used by HasBudget and by the meters
// endpoint's total_cost_usd field.
func (s *Store) GetTotalForPeriod(
	ctx context.Context,
	tenantID string,
	periodStart, periodEnd time.Time,
) (float64, error) {
	if !s.HasPool() {
		return 0, nil
	}
	// RLS: bind the tenant or the force policy hides the rows and every
	// budget check sees $0 spent (fail-open).
	if strings.TrimSpace(tenantID) != "" {
		ctx = tenantctx.WithTenant(ctx, tenantID, "")
	}
	var total float64
	err := s.pool.QueryRow(ctx, `
		select coalesce(sum(cost_usd), 0)::float8
		  from suite_usage_meters
		 where tenant_id = $1
		   and period_start >= $2
		   and period_start <  $3
	`, tenantID, periodStart.UTC(), periodEnd.UTC()).Scan(&total)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("billing: get total: %w", err)
	}
	return total, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func scanCustomer(rows pgx.Rows) (Customer, error) {
	var (
		tenantID           string
		stripeCustomerID   *string
		email              *string
		plan               string
		trialEndsAt        *time.Time
		currentPeriodEnd   *time.Time
		subscriptionStatus *string
		createdAt          time.Time
		updatedAt          time.Time
	)
	if err := rows.Scan(
		&tenantID, &stripeCustomerID, &email, &plan,
		&trialEndsAt, &currentPeriodEnd, &subscriptionStatus,
		&createdAt, &updatedAt,
	); err != nil {
		return Customer{}, fmt.Errorf("billing: scan customer: %w", err)
	}
	c := Customer{
		TenantID:           tenantID,
		StripeCustomerID:   stripeCustomerID,
		Email:              email,
		Plan:               plan,
		SubscriptionStatus: subscriptionStatus,
		CreatedAt:          createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:          updatedAt.UTC().Format(time.RFC3339Nano),
	}
	if trialEndsAt != nil {
		s := trialEndsAt.UTC().Format(time.RFC3339Nano)
		c.TrialEndsAt = &s
	}
	if currentPeriodEnd != nil {
		s := currentPeriodEnd.UTC().Format(time.RFC3339Nano)
		c.CurrentPeriodEnd = &s
	}
	return c, nil
}

func scanMeter(rows pgx.Rows) (UsageMeter, error) {
	var (
		meter         string
		tenantID      string
		periodStart   time.Time
		periodEnd     time.Time
		quantity      float64
		costUSD       *float64
		stripeMeterID *string
		lastSyncedAt  *time.Time
	)
	if err := rows.Scan(
		&meter, &tenantID, &periodStart, &periodEnd,
		&quantity, &costUSD, &stripeMeterID, &lastSyncedAt,
	); err != nil {
		return UsageMeter{}, fmt.Errorf("billing: scan meter: %w", err)
	}
	m := UsageMeter{
		Meter:         meter,
		TenantID:      tenantID,
		PeriodStart:   periodStart.UTC().Format(time.RFC3339Nano),
		PeriodEnd:     periodEnd.UTC().Format(time.RFC3339Nano),
		Quantity:      quantity,
		CostUSD:       costUSD,
		StripeMeterID: stripeMeterID,
	}
	if lastSyncedAt != nil {
		s := lastSyncedAt.UTC().Format(time.RFC3339Nano)
		m.LastSyncedAt = &s
	}
	return m, nil
}

// parseRFC3339 accepts both RFC3339 and RFC3339Nano, matching the
// convention used by services/runtime/internal/server/secrets.go.
func parseRFC3339(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
