// SPDX-License-Identifier: Apache-2.0

// plans.go — the operator-editable pricing catalog (suite_billing_plans).
//
// A Plan names a tier, optionally binds it to a Stripe Price, declares the
// monthly LLM budget the runtime enforces for tenants on it, and carries a
// freeform entitlements object app code reads via
// GET /api/v1/billing/entitlements (e.g. {"simulations": 3}).
//
// The catalog is global (not tenant data): reads are public — pricing
// pages need them — and writes are operator-gated at the API layer.

package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Plan mirrors PlanSchema.
type Plan struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	StripePriceID *string        `json:"stripe_price_id"`
	PriceUSDMonth float64        `json:"price_usd_month"`
	LLMBudgetUSD  *float64       `json:"llm_budget_usd"`
	Entitlements  map[string]any `json:"entitlements"`
	IsDefault     bool           `json:"is_default"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

// ErrPlanNotFound is returned by GetPlan when neither the requested id nor
// a default plan exists.
var ErrPlanNotFound = errors.New("billing: plan not found")

const planCols = `
	id, name, stripe_price_id, price_usd_month, llm_budget_usd,
	entitlements, is_default, created_at, updated_at`

func scanPlan(row pgx.Row) (Plan, error) {
	var (
		p         Plan
		ents      []byte
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(
		&p.ID, &p.Name, &p.StripePriceID, &p.PriceUSDMonth, &p.LLMBudgetUSD,
		&ents, &p.IsDefault, &createdAt, &updatedAt,
	); err != nil {
		return Plan{}, err
	}
	p.Entitlements = map[string]any{}
	if len(ents) > 0 {
		if err := json.Unmarshal(ents, &p.Entitlements); err != nil {
			return Plan{}, fmt.Errorf("billing: decode entitlements for plan %s: %w", p.ID, err)
		}
	}
	p.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	p.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return p, nil
}

// ListPlans returns the catalog, default first then by ascending price.
func (s *Store) ListPlans(ctx context.Context) ([]Plan, error) {
	if !s.HasPool() {
		return []Plan{}, nil
	}
	rows, err := s.pool.Query(ctx, `select`+planCols+`
		from suite_billing_plans
		order by is_default desc, price_usd_month asc, id asc`)
	if err != nil {
		return nil, fmt.Errorf("billing: list plans: %w", err)
	}
	defer rows.Close()
	out := make([]Plan, 0)
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("billing: iterate plans: %w", err)
	}
	return out, nil
}

// GetPlan resolves id, falling back to the default plan when id is empty
// or unknown — so a tenant whose plan was deleted still resolves.
func (s *Store) GetPlan(ctx context.Context, id string) (Plan, error) {
	if !s.HasPool() {
		return Plan{}, ErrPlanNotFound
	}
	id = strings.TrimSpace(id)
	if id != "" {
		p, err := scanPlan(s.pool.QueryRow(ctx,
			`select`+planCols+` from suite_billing_plans where id = $1`, id))
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Plan{}, fmt.Errorf("billing: get plan %s: %w", id, err)
		}
	}
	p, err := scanPlan(s.pool.QueryRow(ctx,
		`select`+planCols+` from suite_billing_plans where is_default limit 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("billing: get default plan: %w", err)
	}
	return p, nil
}

// GetPlanExact resolves a plan by id with NO default fallback: an empty
// or unknown id returns ErrPlanNotFound. Use this for caller-supplied
// plan ids (e.g. checkout) where silently substituting the default plan
// would subscribe the tenant to the wrong plan. GetPlan keeps the
// default-fallback semantics for resolving a tenant's *current* plan.
func (s *Store) GetPlanExact(ctx context.Context, id string) (Plan, error) {
	if !s.HasPool() {
		return Plan{}, ErrPlanNotFound
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Plan{}, ErrPlanNotFound
	}
	p, err := scanPlan(s.pool.QueryRow(ctx,
		`select`+planCols+` from suite_billing_plans where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("billing: get plan %s: %w", id, err)
	}
	return p, nil
}

// PlanByStripePrice resolves the plan bound to a Stripe Price id — the
// webhook handler uses this to map subscription events back to a plan.
func (s *Store) PlanByStripePrice(ctx context.Context, priceID string) (Plan, error) {
	if !s.HasPool() || strings.TrimSpace(priceID) == "" {
		return Plan{}, ErrPlanNotFound
	}
	p, err := scanPlan(s.pool.QueryRow(ctx,
		`select`+planCols+` from suite_billing_plans where stripe_price_id = $1 limit 1`,
		priceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("billing: plan by price %s: %w", priceID, err)
	}
	return p, nil
}

// UpsertPlan creates or updates a catalog row. Setting IsDefault moves the
// default flag atomically (at most one default, enforced by a partial
// unique index).
func (s *Store) UpsertPlan(ctx context.Context, p Plan) (Plan, error) {
	p.ID = strings.TrimSpace(strings.ToLower(p.ID))
	p.Name = strings.TrimSpace(p.Name)
	if p.ID == "" || p.Name == "" {
		return Plan{}, fmt.Errorf("%w: plan id and name are required", ErrInvalidInput)
	}
	if p.PriceUSDMonth < 0 {
		return Plan{}, fmt.Errorf("%w: price must be non-negative", ErrInvalidInput)
	}
	if p.LLMBudgetUSD != nil && *p.LLMBudgetUSD <= 0 {
		return Plan{}, fmt.Errorf("%w: llm_budget_usd must be positive when set", ErrInvalidInput)
	}
	if !s.HasPool() {
		return Plan{}, fmt.Errorf("%w: no database", ErrBillingUnavailable)
	}
	if p.Entitlements == nil {
		p.Entitlements = map[string]any{}
	}
	ents, err := json.Marshal(p.Entitlements)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: entitlements must be JSON-encodable", ErrInvalidInput)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("billing: upsert plan begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if p.IsDefault {
		if _, err := tx.Exec(ctx,
			`update suite_billing_plans set is_default = false, updated_at = now()
			  where is_default and id <> $1`, p.ID); err != nil {
			return Plan{}, fmt.Errorf("billing: move default: %w", err)
		}
	}
	row := tx.QueryRow(ctx, `
		insert into suite_billing_plans
			(id, name, stripe_price_id, price_usd_month, llm_budget_usd, entitlements, is_default)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (id) do update set
			name = excluded.name,
			stripe_price_id = excluded.stripe_price_id,
			price_usd_month = excluded.price_usd_month,
			llm_budget_usd = excluded.llm_budget_usd,
			entitlements = excluded.entitlements,
			is_default = excluded.is_default,
			updated_at = now()
		returning`+planCols,
		p.ID, p.Name, p.StripePriceID, p.PriceUSDMonth, p.LLMBudgetUSD, ents, p.IsDefault)
	out, err := scanPlan(row)
	if err != nil {
		return Plan{}, fmt.Errorf("billing: upsert plan %s: %w", p.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, fmt.Errorf("billing: upsert plan commit: %w", err)
	}
	return out, nil
}

// DeletePlan removes a plan. The default plan cannot be deleted (tenants
// must always resolve somewhere).
func (s *Store) DeletePlan(ctx context.Context, id string) error {
	if !s.HasPool() {
		return fmt.Errorf("%w: no database", ErrBillingUnavailable)
	}
	tag, err := s.pool.Exec(ctx,
		`delete from suite_billing_plans where id = $1 and not is_default`, id)
	if err != nil {
		return fmt.Errorf("billing: delete plan %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: plan %s (default plans cannot be deleted)", ErrPlanNotFound, id)
	}
	return nil
}
