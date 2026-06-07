// SPDX-License-Identifier: Apache-2.0

// budgets.go — per-tenant monthly budget store + spend lookup.
//
// One Budgets is constructed per process and shared between:
//   - the hooks.HookLLMPreCall handler (HasBudget gate),
//   - the admin REST surface (/api/v1/admin/budgets, Get/Set/List),
//   - the dashboard cost summary (BudgetUSD field).
//
// A nil pool means "no DB" — every method returns an empty/permissive
// result so the gateway boot path doesn't need special-casing.

package cost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Budgets stores and reads per-tenant monthly budgets.
type Budgets struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewBudgets constructs a Budgets. Pool/log may be nil — in nil-pool
// mode every method short-circuits with empty/permissive results.
func NewBudgets(pool *pgxpool.Pool, log *slog.Logger) *Budgets {
	if log == nil {
		log = slog.Default()
	}
	return &Budgets{pool: pool, log: log}
}

// Get returns the budget for tenantID. Returns ErrBudgetNotFound when
// no row exists. SpentThisPeriodUSD/RemainingUSD are computed live by
// joining against suite_cost_events; ResetsAt is one calendar month
// after CurrentPeriodStart.
func (b *Budgets) Get(ctx context.Context, tenantID string) (Budget, error) {
	if b == nil || b.pool == nil {
		return Budget{}, ErrBudgetNotFound
	}
	if tenantID == "" {
		return Budget{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}

	var (
		monthlyUSD     float64
		alertThreshold int
		periodStart    time.Time
	)
	err := b.pool.QueryRow(ctx, `
        select monthly_usd, alert_threshold_pct, current_period_start
          from suite_budgets
         where tenant_id = $1
    `, tenantID).Scan(&monthlyUSD, &alertThreshold, &periodStart)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Budget{}, ErrBudgetNotFound
		}
		return Budget{}, fmt.Errorf("cost: load budget: %w", err)
	}

	spent, err := b.Spent(ctx, tenantID, periodStart)
	if err != nil {
		return Budget{}, err
	}
	return buildBudget(tenantID, monthlyUSD, alertThreshold, periodStart, spent), nil
}

// Set creates or updates a tenant budget. PUT semantics: upsert keyed
// on tenant_id. updated_at refreshes; current_period_start is left
// alone on update so changing the cap mid-month doesn't reset the
// running spend.
//
// On insert, current_period_start is set to date_trunc('month', now()).
func (b *Budgets) Set(ctx context.Context, in SetBudgetInput) (Budget, error) {
	if b == nil || b.pool == nil {
		return Budget{}, errors.New("cost: budgets storage unavailable")
	}
	if in.TenantID == "" {
		return Budget{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	if in.MonthlyUSD <= 0 {
		return Budget{}, fmt.Errorf("%w: monthly_usd must be positive", ErrInvalidInput)
	}
	// Schema default is 80; admin handler propagates it. Clamp here as
	// a defence against direct callers in tests.
	thresh := in.AlertThresholdPct
	if thresh == 0 {
		thresh = 80
	}
	if thresh < 0 || thresh > 100 {
		return Budget{}, fmt.Errorf("%w: alert_threshold_pct must be 0..100", ErrInvalidInput)
	}

	var periodStart time.Time
	err := b.pool.QueryRow(ctx, `
        insert into suite_budgets
            (tenant_id, monthly_usd, alert_threshold_pct)
        values ($1, $2, $3)
        on conflict (tenant_id) do update set
            monthly_usd = excluded.monthly_usd,
            alert_threshold_pct = excluded.alert_threshold_pct,
            updated_at = now()
        returning current_period_start
    `, in.TenantID, in.MonthlyUSD, thresh).Scan(&periodStart)
	if err != nil {
		return Budget{}, fmt.Errorf("cost: upsert budget: %w", err)
	}

	spent, err := b.Spent(ctx, in.TenantID, periodStart)
	if err != nil {
		return Budget{}, err
	}
	return buildBudget(in.TenantID, in.MonthlyUSD, thresh, periodStart, spent), nil
}

// List returns every budget joined with the current period's spend.
// Powers GET /api/v1/admin/budgets.
func (b *Budgets) List(ctx context.Context) ([]Budget, error) {
	if b == nil || b.pool == nil {
		return []Budget{}, nil
	}
	rows, err := b.pool.Query(ctx, `
        select
            tenant_id::text,
            monthly_usd,
            alert_threshold_pct,
            current_period_start,
            coalesce((
              select sum(cost_usd)
                from suite_cost_events
               where suite_cost_events.tenant_id = suite_budgets.tenant_id
                 and occurred_at >= suite_budgets.current_period_start
                 and occurred_at < suite_budgets.current_period_start + interval '1 month'
            ), 0) as spent
        from suite_budgets
        order by tenant_id
    `)
	if err != nil {
		return nil, fmt.Errorf("cost: list budgets: %w", err)
	}
	defer rows.Close()

	out := make([]Budget, 0)
	for rows.Next() {
		var (
			tenantID       string
			monthlyUSD     float64
			alertThreshold int
			periodStart    time.Time
			spent          float64
		)
		if err := rows.Scan(&tenantID, &monthlyUSD, &alertThreshold, &periodStart, &spent); err != nil {
			return nil, fmt.Errorf("cost: scan budget row: %w", err)
		}
		out = append(out, buildBudget(tenantID, monthlyUSD, alertThreshold, periodStart, spent))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost: iterate budget rows: %w", err)
	}
	return out, nil
}

// Spent returns the total cost_usd for tenantID over
// [periodStart, periodStart + 1 month). Returns 0 when no events.
//
// Exposed (vs. inlined) so the dashboard cost summary can compute
// "current period total" without re-running the cost-events query.
func (b *Budgets) Spent(ctx context.Context, tenantID string, periodStart time.Time) (float64, error) {
	if b == nil || b.pool == nil {
		return 0, nil
	}
	if tenantID == "" {
		return 0, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	var spent float64
	err := b.pool.QueryRow(ctx, `
        select coalesce(sum(cost_usd), 0)
          from suite_cost_events
         where tenant_id = $1
           and occurred_at >= $2
           and occurred_at < $2 + interval '1 month'
    `, tenantID, periodStart).Scan(&spent)
	if err != nil {
		return 0, fmt.Errorf("cost: load spend: %w", err)
	}
	return spent, nil
}

// HasBudget is the gateway pre-call gate.
//
// Returns (true, nil) when:
//   - no DB / no budgets at all (permissive boot mode),
//   - tenantID has no budget row (no cap configured),
//   - spent + estimated <= monthly_usd.
//
// Returns (false, nil) when the call would exceed the cap. Returns
// (false, err) only on a real database error — the gateway treats this
// as fail-open (admit the call) so a flaky PG doesn't take down the
// LLM surface.
//
// estimatedUSD is the caller's best guess at the upcoming call's cost.
// The gateway typically passes a small sentinel (~$0.001) so the gate
// fires on "already over" rather than predictive overshoot.
func (b *Budgets) HasBudget(ctx context.Context, tenantID string, estimatedUSD float64) (bool, error) {
	if b == nil || b.pool == nil {
		return true, nil
	}
	if tenantID == "" {
		// No tenant -> no per-tenant cap applies.
		return true, nil
	}

	var (
		monthlyUSD  float64
		periodStart time.Time
	)
	err := b.pool.QueryRow(ctx, `
        select monthly_usd, current_period_start
          from suite_budgets
         where tenant_id = $1
    `, tenantID).Scan(&monthlyUSD, &periodStart)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No cap configured for this tenant -> allow.
			return true, nil
		}
		return false, fmt.Errorf("cost: lookup budget: %w", err)
	}

	spent, err := b.Spent(ctx, tenantID, periodStart)
	if err != nil {
		return false, err
	}
	if spent+estimatedUSD > monthlyUSD {
		return false, nil
	}
	return true, nil
}

// buildBudget shapes the in-memory Budget from the row + spend.
//
// ResetsAt rolls CurrentPeriodStart forward by exactly one calendar
// month (using AddDate(0, 1, 0) which handles month-length variation
// — 31 Jan -> 28/29 Feb). RemainingUSD floors at 0 so the dashboard
// doesn't render negative budget remaining.
func buildBudget(tenantID string, monthlyUSD float64, alertThreshold int, periodStart time.Time, spent float64) Budget {
	remaining := monthlyUSD - spent
	if remaining < 0 {
		remaining = 0
	}
	return Budget{
		TenantID:           tenantID,
		MonthlyUSD:         monthlyUSD,
		AlertThresholdPct:  alertThreshold,
		SpentThisPeriodUSD: spent,
		RemainingUSD:       remaining,
		ResetsAt:           periodStart.AddDate(0, 1, 0).UTC(),
		CurrentPeriodStart: periodStart.UTC(),
	}
}