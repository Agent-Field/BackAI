// SPDX-License-Identifier: Apache-2.0

// service.go — public Service surface combining the Store + the Stripe
// Client. The REST layer and the SDKs talk to this — they don't reach
// into Store or Client directly.
//
// Responsibilities:
//
//   - Read paths (ListCustomers, GetCustomer, ListMeters) translate the
//     Store rows into wire shapes, applying RLS bypass when the caller
//     is asking for a cross-tenant listing.
//   - Meter (write path) computes the current period boundary, applies
//     the meter's price (if registered), and UPSERTs through the Store.
//   - HasBudget is the per-tenant pre-call gate — analogous to
//     cost.Budgets.HasBudget. It looks up the customer's plan, applies a
//     plan-default cap, and compares against the current period's spend.
//   - PortalLink defers to the Stripe Client, after looking up the
//     customer's stripe_customer_id and provisioning one if absent.
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// MeterPrice records the USD price-per-unit for a registered meter. nil
// price (or absent entry) means the meter is informational — quantity
// accumulates but cost_usd stays NULL.
type MeterPrice = float64

// MeterRegistry maps meter name -> price-per-unit (USD). Pre-populated
// at boot with the well-known meters; new meters can be registered at
// runtime via Service.RegisterMeter.
type MeterRegistry struct {
	prices map[string]MeterPrice
}

// NewMeterRegistry seeds the registry with the canonical AF Stack meters.
// Prices here are conservative defaults; real deployments should
// override via cost.* config.
func NewMeterRegistry() *MeterRegistry {
	return &MeterRegistry{
		prices: map[string]MeterPrice{
			// sandbox: cpu-seconds — matches sandbox.CostPerCPUSecond.
			"sandbox_seconds": 0.00005,
			// llm: tokens — best-effort default; the LLM gateway emits
			// authoritative cost via cost.Recorder, so this meter is
			// mostly informational.
			"llm_tokens": 0,
			// storage: bytes — informational; storage spend is typically
			// metered by the underlying bucket provider, not us.
			"storage_bytes": 0,
		},
	}
}

// Price returns the registered USD price-per-unit for a meter, or 0 if
// the meter is unpriced (informational).
func (r *MeterRegistry) Price(name string) MeterPrice {
	if r == nil || r.prices == nil {
		return 0
	}
	return r.prices[name]
}

// Register adds or overwrites a meter's price. Safe to call at boot.
func (r *MeterRegistry) Register(name string, priceUSD MeterPrice) {
	if r == nil {
		return
	}
	if r.prices == nil {
		r.prices = map[string]MeterPrice{}
	}
	r.prices[name] = priceUSD
}

// Service is the public billing API.
type Service struct {
	store  *Store
	client Client
	meters *MeterRegistry
	log    *slog.Logger
	// planBudget maps plan name -> monthly USD cap (0 = unlimited).
	// Used by HasBudget. Overridable via RegisterPlanBudget; defaults
	// come from defaultPlanBudgets() below.
	planBudgets map[string]float64
}

// NewService constructs a Service.
//
//   - store may be nil — Service degrades to "no persistence", returning
//     empty reads and silently dropping writes (the REST layer still
//     responds 200).
//   - client may be nil — PortalLink returns ErrStripeUnavailable.
//   - meters may be nil — NewMeterRegistry() is used as the default.
//   - log defaults to slog.Default() when nil.
func NewService(store *Store, client Client, meters *MeterRegistry, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if meters == nil {
		meters = NewMeterRegistry()
	}
	return &Service{
		store:       store,
		client:      client,
		meters:      meters,
		log:         log,
		planBudgets: defaultPlanBudgets(),
	}
}

// defaultPlanBudgets is the per-plan monthly USD cap used by HasBudget.
// 0 means "no cap" (enterprise). free is intentionally low so the gate
// triggers in dev when the runtime is mistakenly serving real traffic
// on a free tenant.
func defaultPlanBudgets() map[string]float64 {
	return map[string]float64{
		"free":       10.0,
		"pro":        500.0,
		"enterprise": 0, // unlimited
	}
}

// RegisterPlanBudget overrides the cap for a plan. Used by tests + by
// deployments that want to wire caps from config.
func (s *Service) RegisterPlanBudget(plan string, monthlyUSD float64) {
	if s == nil {
		return
	}
	if s.planBudgets == nil {
		s.planBudgets = map[string]float64{}
	}
	s.planBudgets[plan] = monthlyUSD
}

// ─── Read surface ─────────────────────────────────────────────────────────

// ListCustomers returns every billing customer. The REST handler calls
// SetBypassRLS on the session before invoking this; without it the
// listing is silently filtered to a single tenant.
func (s *Service) ListCustomers(ctx context.Context) ([]Customer, error) {
	if s == nil || s.store == nil {
		return []Customer{}, nil
	}
	return s.store.ListCustomers(ctx)
}

// GetCustomer returns the billing row for tenantID. Returns ErrNotFound
// when no row exists — the REST handler may translate that to a
// synthesised "free plan" row.
func (s *Service) GetCustomer(ctx context.Context, tenantID string) (Customer, error) {
	if s == nil || s.store == nil {
		return Customer{}, ErrNotFound
	}
	return s.store.GetCustomer(ctx, tenantID)
}

// ListMeters returns meters for the given filters. When TenantID is
// empty the listing is cross-tenant (REST handler is expected to have
// set bypass_rls).
func (s *Service) ListMeters(ctx context.Context, f MeterFilters) ([]UsageMeter, error) {
	if s == nil || s.store == nil {
		return []UsageMeter{}, nil
	}
	return s.store.ListMeters(ctx, f)
}

// ─── Write surface ────────────────────────────────────────────────────────

// Meter records qty units of meter for tenantID in the current month
// bucket. The cost_usd column is bumped by qty*price for priced meters.
//
// Safe to call from hot paths — falls through silently when store is
// nil. tenantID="" is a no-op (no per-tenant aggregation possible).
func (s *Service) Meter(ctx context.Context, name string, qty float64, tenantID string) error {
	if s == nil || s.store == nil || !s.store.HasPool() {
		return nil
	}
	name = strings.TrimSpace(name)
	tenantID = strings.TrimSpace(tenantID)
	if name == "" || tenantID == "" {
		return nil
	}
	if qty < 0 {
		return fmt.Errorf("%w: quantity must be non-negative", ErrInvalidInput)
	}
	start, end := PeriodBoundary(BucketMonth, time.Now())
	return s.store.IncrementMeter(ctx, name, tenantID, start, end, qty, s.meters.Price(name))
}

// HasBudget reports whether tenantID has room for an additional
// additionalUSD of spend this period.
//
// Returns (true, nil) when:
//   - no store wired (boot mode),
//   - no plan cap configured (enterprise / unknown plan),
//   - existing spend + additional <= cap.
//
// Returns (false, nil) when the additional spend would breach the cap.
// Returns (false, err) only on DB error. The caller (LLM gateway / sandbox)
// is expected to fail-open on err so a flaky DB doesn't tank the surface.
func (s *Service) HasBudget(ctx context.Context, tenantID string, additionalUSD float64) (bool, error) {
	if s == nil || s.store == nil || !s.store.HasPool() {
		return true, nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return true, nil
	}
	cust, err := s.store.GetCustomer(ctx, tenantID)
	plan := "free"
	if err == nil {
		plan = cust.Plan
	}
	cap, ok := s.planBudgets[plan]
	if !ok || cap <= 0 {
		// Unknown plan -> permissive (enterprise == 0 == unlimited).
		return true, nil
	}
	start, end := PeriodBoundary(BucketMonth, time.Now())
	spent, err := s.store.GetTotalForPeriod(ctx, tenantID, start, end)
	if err != nil {
		return false, err
	}
	if spent+additionalUSD > cap {
		return false, nil
	}
	return true, nil
}

// PortalLink mints a Stripe Customer Portal URL for tenantID.
//
// When the tenant doesn't have a stripe_customer_id yet, this provisions
// one via Client.CreateCustomer (using the customer's email if known),
// upserts the row, and then mints the portal link.
//
// In stub mode, the URL is deterministic and points at example.com so
// the dashboard can still render the button.
func (s *Service) PortalLink(ctx context.Context, tenantID, returnURL string) (PortalLink, error) {
	if s == nil || s.client == nil {
		return PortalLink{}, fmt.Errorf("%w: stripe client not configured", ErrStripeUnavailable)
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return PortalLink{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}

	var stripeID string
	var email string
	if s.store != nil && s.store.HasPool() {
		cust, err := s.store.GetCustomer(ctx, tenantID)
		if err == nil {
			if cust.StripeCustomerID != nil {
				stripeID = *cust.StripeCustomerID
			}
			if cust.Email != nil {
				email = *cust.Email
			}
		}
	}

	if stripeID == "" {
		newID, err := s.client.CreateCustomer(email)
		if err != nil {
			return PortalLink{}, err
		}
		stripeID = newID
		// Upsert so the next portal call short-circuits to the existing id.
		if s.store != nil && s.store.HasPool() {
			c := Customer{
				TenantID:         tenantID,
				StripeCustomerID: &stripeID,
				Plan:             "free",
			}
			if email != "" {
				c.Email = &email
			}
			if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
				// Non-fatal: portal still works, we just couldn't cache.
				s.log.Warn("billing: upsert after stripe provision failed",
					"tenant", tenantID, "error", err)
			}
		}
	}

	return s.client.CreatePortalLink(stripeID, returnURL)
}

// Client returns the underlying Stripe client. Exposed so the webhook
// handler can verify signatures without re-reading the env var.
func (s *Service) Client() Client {
	if s == nil {
		return nil
	}
	return s.client
}

// Store returns the underlying Store. Exposed so the webhook handler
// can upsert customers without re-deriving them.
func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}