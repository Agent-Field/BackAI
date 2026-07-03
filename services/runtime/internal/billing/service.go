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
	"sync"
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
	store *Store
	// mu guards client, which the operator panel can hot-swap at
	// runtime (SwapClient) when Stripe keys are saved from the UI.
	mu     sync.RWMutex
	client Client
	meters *MeterRegistry
	log    *slog.Logger
	// planBudget maps plan name -> monthly USD cap (0 = unlimited).
	// Used by HasBudget. Overridable via RegisterPlanBudget; defaults
	// come from defaultPlanBudgets() below.
	planBudgets map[string]float64
	// onPlanApplied, when set (SetOnPlanApplied), runs after a plan is
	// applied to a tenant — by a subscription webhook or a stub-mode
	// checkout. The server wires this to push the plan's enforced LLM
	// budget (cost.Budgets) so plan changes take effect immediately.
	onPlanApplied func(ctx context.Context, tenantID string, plan Plan)
}

// SetOnPlanApplied registers the plan-application hook. Call before
// serving traffic (not synchronized with firePlanApplied).
func (s *Service) SetOnPlanApplied(fn func(ctx context.Context, tenantID string, plan Plan)) {
	if s == nil {
		return
	}
	s.onPlanApplied = fn
}

// firePlanApplied invokes the hook when registered.
func (s *Service) firePlanApplied(ctx context.Context, tenantID string, plan Plan) {
	if s == nil || s.onPlanApplied == nil {
		return
	}
	s.onPlanApplied(ctx, tenantID, plan)
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

// AdapterName returns the configured external billing adapter.
func (s *Service) AdapterName() string {
	c := s.Client()
	if c == nil {
		return "none"
	}
	return c.AdapterName()
}

// PortalLink mints a billing-provider customer portal URL for tenantID.
//
// When the tenant doesn't have an external billing customer id yet, this
// provisions one via Client.CreateCustomer (using the customer's email
// if known), upserts the row, and then mints the portal link.
//
// In stub mode, the URL is deterministic and points at example.com so
// the dashboard can still render the button.
func (s *Service) PortalLink(ctx context.Context, tenantID, returnURL string) (PortalLink, error) {
	client := s.Client()
	if client == nil {
		return PortalLink{}, fmt.Errorf("%w: billing adapter not configured", ErrBillingUnavailable)
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return PortalLink{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}

	var externalCustomerID string
	var email string
	if s.store != nil && s.store.HasPool() {
		cust, err := s.store.GetCustomer(ctx, tenantID)
		if err == nil {
			if cust.StripeCustomerID != nil {
				externalCustomerID = *cust.StripeCustomerID
			}
			if cust.Email != nil {
				email = *cust.Email
			}
		}
	}

	if externalCustomerID == "" {
		newID, err := client.CreateCustomer(ctx, tenantID, email)
		if err != nil {
			return PortalLink{}, err
		}
		externalCustomerID = newID
		// Upsert so the next portal call short-circuits to the existing id.
		if s.store != nil && s.store.HasPool() {
			c := Customer{
				TenantID:         tenantID,
				StripeCustomerID: &externalCustomerID,
				Plan:             "free",
			}
			if email != "" {
				c.Email = &email
			}
			if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
				// Non-fatal: portal still works, we just couldn't cache.
				s.log.Warn("billing: upsert after external customer provision failed",
					"tenant", tenantID, "error", err)
			}
		}
	}

	return client.CreatePortalLink(ctx, externalCustomerID, returnURL)
}

// Client returns the underlying billing adapter. Exposed so the webhook
// handler can verify signatures without re-reading the env var.
func (s *Service) Client() Client {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// SwapClient atomically replaces the billing adapter. The operator
// panel calls this (via the admin billing-settings endpoint) after
// writing new Stripe keys to the secrets vault, so key changes take
// effect without a restart.
func (s *Service) SwapClient(c Client) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
}

// Store returns the underlying Store. Exposed so the webhook handler
// can upsert customers without re-deriving them.
func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}

// ─── Plans catalog + checkout ─────────────────────────────────────────────

// Plans returns the operator-editable pricing catalog.
func (s *Service) Plans(ctx context.Context) ([]Plan, error) {
	if s == nil || s.store == nil {
		return []Plan{}, nil
	}
	return s.store.ListPlans(ctx)
}

// PlanFor resolves the plan a tenant is on: customers.plan -> catalog,
// falling back to the default plan for unknown/absent rows.
func (s *Service) PlanFor(ctx context.Context, tenantID string) (Plan, error) {
	if s == nil || s.store == nil {
		return Plan{}, ErrPlanNotFound
	}
	planID := ""
	if cust, err := s.store.GetCustomer(ctx, tenantID); err == nil {
		planID = cust.Plan
	}
	return s.store.GetPlan(ctx, planID)
}

// GetPlan resolves a plan id (default fallback semantics — see Store.GetPlan).
func (s *Service) GetPlan(ctx context.Context, id string) (Plan, error) {
	if s == nil || s.store == nil {
		return Plan{}, ErrPlanNotFound
	}
	return s.store.GetPlan(ctx, id)
}

// PlanByStripePrice maps a Stripe Price id to a catalog plan.
func (s *Service) PlanByStripePrice(ctx context.Context, priceID string) (Plan, error) {
	if s == nil || s.store == nil {
		return Plan{}, ErrPlanNotFound
	}
	return s.store.PlanByStripePrice(ctx, priceID)
}

// UpsertPlan writes a catalog row and keeps the in-process plan-budget
// registry (HasBudget) in sync with it.
func (s *Service) UpsertPlan(ctx context.Context, p Plan) (Plan, error) {
	if s == nil || s.store == nil {
		return Plan{}, fmt.Errorf("%w: no database", ErrBillingUnavailable)
	}
	// Agent-first: when Stripe is live and this is a paid plan with no
	// Price yet, provision the Stripe Product + Price automatically so
	// nobody has to touch the Stripe dashboard. Stub mode skips this —
	// its checkout applies plans directly.
	if p.PriceUSDMonth > 0 && (p.StripePriceID == nil || strings.TrimSpace(*p.StripePriceID) == "") {
		client := s.Client()
		if client != nil && !client.IsStub() {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				name = p.ID
			}
			cents := int64(p.PriceUSDMonth*100 + 0.5)
			priceID, perr := client.EnsurePrice(ctx, p.ID, name, cents, "usd")
			if perr != nil {
				return Plan{}, fmt.Errorf("provision stripe price for plan %q: %w", p.ID, perr)
			}
			p.StripePriceID = &priceID
		}
	}
	out, err := s.store.UpsertPlan(ctx, p)
	if err != nil {
		return Plan{}, err
	}
	if out.LLMBudgetUSD != nil {
		s.RegisterPlanBudget(out.ID, *out.LLMBudgetUSD)
	}
	return out, nil
}

// paidPlans returns the subset of a catalog that carries a Stripe price
// (price > 0). Free plans have no Stripe object, so they're never
// reconciled. Pure so it can be unit-tested without a database.
func paidPlans(plans []Plan) []Plan {
	out := make([]Plan, 0, len(plans))
	for _, p := range plans {
		if p.PriceUSDMonth > 0 {
			out = append(out, p)
		}
	}
	return out
}

// ReconcilePlanPrices re-provisions every paid plan's Stripe Price under
// the currently-active client. A stored stripe_price_id is only valid for
// the exact Stripe key (account + mode) that created it: when an operator
// swaps keys — most commonly flipping live↔test — the old prices 404 at
// checkout ("No such price … a similar object exists in live mode, but a
// test mode key was used"). The operator billing-settings handler calls
// this after a key change so the catalog re-points at fresh Product+Price
// objects under the new key and checkout keeps working without anyone
// touching the Stripe dashboard.
//
// Stub mode is a no-op — stub checkout applies plans directly and ignores
// prices. Best-effort per plan: if re-provisioning a plan fails under the
// new key, its stale price is cleared (set NULL) so checkout returns a
// clean "no price" path instead of a wrong-mode 404, and the remaining
// plans are still reconciled. Returns the ids of the plans whose price was
// successfully re-provisioned.
func (s *Service) ReconcilePlanPrices(ctx context.Context) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	client := s.Client()
	if client == nil || client.IsStub() {
		return nil, nil
	}
	plans, err := s.store.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	reconciled := make([]string, 0)
	for _, p := range paidPlans(plans) {
		// Clearing the price makes UpsertPlan re-provision it under the
		// active client, minting a fresh Price on the new key.
		cleared := p
		cleared.StripePriceID = nil
		updated, uerr := s.UpsertPlan(ctx, cleared)
		if uerr != nil {
			// Provisioning failed under the new key. Persist the cleared
			// price directly so checkout fails cleanly rather than hitting
			// the old key's now-invalid price, then keep going.
			if _, cerr := s.store.UpsertPlan(ctx, cleared); cerr != nil {
				s.log.Warn("billing: reconcile could not clear stale price",
					"plan", p.ID, "provision_err", uerr, "clear_err", cerr)
			} else {
				s.log.Warn("billing: reconcile cleared stale price (re-provision failed)",
					"plan", p.ID, "err", uerr)
			}
			continue
		}
		if updated.StripePriceID != nil && *updated.StripePriceID != "" {
			reconciled = append(reconciled, updated.ID)
		}
	}
	if len(reconciled) > 0 {
		s.log.Info("billing: reconciled plan prices after key change", "plans", reconciled)
	}
	return reconciled, nil
}

// DeletePlan removes a catalog row (the default plan is protected).
func (s *Service) DeletePlan(ctx context.Context, id string) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: no database", ErrBillingUnavailable)
	}
	return s.store.DeletePlan(ctx, id)
}

// SyncPlanBudgets loads the catalog and registers each plan's LLM budget
// with the in-process HasBudget registry. Called at boot and after
// catalog edits so meter-based caps track the operator's catalog.
func (s *Service) SyncPlanBudgets(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	plans, err := s.store.ListPlans(ctx)
	if err != nil {
		s.log.Warn("billing: plan budget sync failed", "error", err)
		return
	}
	for _, p := range plans {
		if p.LLMBudgetUSD != nil {
			s.RegisterPlanBudget(p.ID, *p.LLMBudgetUSD)
		}
	}
}

// ensureExternalCustomer returns the tenant's provider customer id,
// provisioning one (and caching it on the Customer row) when absent.
func (s *Service) ensureExternalCustomer(ctx context.Context, client Client, tenantID string) (string, error) {
	var externalID, email string
	if s.store != nil && s.store.HasPool() {
		if cust, err := s.store.GetCustomer(ctx, tenantID); err == nil {
			if cust.StripeCustomerID != nil {
				externalID = *cust.StripeCustomerID
			}
			if cust.Email != nil {
				email = *cust.Email
			}
		}
	}
	if externalID != "" {
		return externalID, nil
	}
	newID, err := client.CreateCustomer(ctx, tenantID, email)
	if err != nil {
		return "", err
	}
	if s.store != nil && s.store.HasPool() {
		c := Customer{TenantID: tenantID, StripeCustomerID: &newID, Plan: "free"}
		if email != "" {
			c.Email = &email
		}
		if _, err := s.store.UpsertCustomer(ctx, c); err != nil {
			s.log.Warn("billing: upsert after external customer provision failed",
				"tenant", tenantID, "error", err)
		}
	}
	return newID, nil
}

// CheckoutResult is what Checkout returns to the REST layer.
type CheckoutResult struct {
	// URL is the hosted checkout page (empty when AppliedDirectly).
	URL string `json:"url"`
	// AppliedDirectly is true in stub mode: there is no provider to
	// check out against, so the plan was applied immediately ("dev
	// checkout") and the caller should treat the purchase as complete.
	AppliedDirectly bool `json:"applied_directly"`
}

// Checkout starts a subscription purchase of plan for tenantID.
//
// Real adapter: mints a hosted checkout session (tenant_id rides on the
// subscription metadata; the webhook handler applies the plan when the
// subscription event lands) and returns its URL.
//
// Stub adapter (no keys configured): applies the plan to the Customer
// row immediately and reports AppliedDirectly — the caller is expected
// to run its plan-application hook (budgets etc.) and finish the flow.
func (s *Service) Checkout(ctx context.Context, tenantID, planID, successURL, cancelURL string) (CheckoutResult, error) {
	client := s.Client()
	if client == nil {
		return CheckoutResult{}, fmt.Errorf("%w: billing adapter not configured", ErrBillingUnavailable)
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return CheckoutResult{}, fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	plan, err := s.GetPlan(ctx, planID)
	if err != nil {
		return CheckoutResult{}, err
	}

	if client.IsStub() {
		if s.store != nil && s.store.HasPool() {
			cust := Customer{TenantID: tenantID, Plan: plan.ID}
			if existing, gerr := s.store.GetCustomer(ctx, tenantID); gerr == nil {
				existing.Plan = plan.ID
				cust = existing
			}
			if _, uerr := s.store.UpsertCustomer(ctx, cust); uerr != nil {
				return CheckoutResult{}, uerr
			}
		}
		s.firePlanApplied(ctx, tenantID, plan)
		return CheckoutResult{AppliedDirectly: true}, nil
	}

	if plan.StripePriceID == nil || strings.TrimSpace(*plan.StripePriceID) == "" {
		return CheckoutResult{}, fmt.Errorf("%w: plan %q has no stripe_price_id", ErrInvalidInput, plan.ID)
	}
	customerID, err := s.ensureExternalCustomer(ctx, client, tenantID)
	if err != nil {
		return CheckoutResult{}, err
	}
	url, err := client.CreateCheckoutSession(ctx, customerID, *plan.StripePriceID, successURL, cancelURL, tenantID)
	if err != nil {
		return CheckoutResult{}, err
	}
	return CheckoutResult{URL: url}, nil
}
