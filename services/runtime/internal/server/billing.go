// SPDX-License-Identifier: Apache-2.0

// billing.go — REST handlers for Phase 10.4 provider-backed billing.
//
// Endpoints map 1:1 to BillingCustomerSchema / BillingCustomerListSchema
// / UsageMeterSchema / UsageMeterListSchema / PortalLinkSchema in
// apps/dashboard/src/lib/api.ts. Any drift here breaks the dashboard's
// safeParse — keep field names snake_case (the billing.Customer struct
// already carries them), keep nullable fields emitted as JSON null
// (pointer fields), and keep arrays non-nil even when empty.
//
// The handlers delegate the real work to billing.Service which composes
// the per-tenant Store + the active billing adapter (Stripe, Lago, or stub). When the
// service isn't wired (no DB at boot), the read endpoints return empty
// pages and the mutating ones return 503 BILLING_NOT_CONFIGURED.
//
// ─── Stripe inbound webhook (coordination with Phase 10.2) ───────────────
//
// The Stripe webhook lands at POST /webhooks/in/stripe. The Phase 10.2
// inbound framework's generic handler at /webhooks/in/{slug} would
// otherwise eat this path, but:
//
//  1. The Go ServeMux exact-match rule means /webhooks/in/stripe (this
//     handler) wins over /webhooks/in/{slug} (the generic handler) for
//     that exact path. Even when Phase 10.2 registers its catch-all,
//     our specific path keeps precedence.
//  2. If a future operator pre-seeds suite_webhook_endpoints with
//     slug='stripe', the inbound framework would never see the request
//     because of #1 — but the seed is still harmless (just unused).
//
// If Phase 10.2 changes the inbound mux pattern to match-all-then-
// dispatch (not the current ServeMux model), this handler must be
// re-coordinated. See services/runtime/internal/server/webhooks.go.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// ─── Wire shapes ──────────────────────────────────────────────────────────

// billingCustomerListResponse mirrors BillingCustomerListSchema.
type billingCustomerListResponse struct {
	Customers []billing.Customer `json:"customers"`
	Adapter   string             `json:"adapter"`
}

// usageMeterListResponse mirrors UsageMeterListSchema.
type usageMeterListResponse struct {
	Meters       []billing.UsageMeter `json:"meters"`
	TotalCostUSD float64              `json:"total_cost_usd"`
}

// portalLinkRequest is the body for POST /billing/customers/{tenantId}/portal.
// return_url is optional; absent means the dashboard's billing tab will
// be used (passed by the SDK).
type portalLinkRequest struct {
	ReturnURL string `json:"return_url,omitempty"`
}

// meterRequest is the body for POST /billing/meter. tenant_id is optional —
// when omitted, the authenticated caller's tenant (from the API key) is used,
// which is the normal app-code path.
type meterRequest struct {
	Name     string  `json:"name"`
	Qty      float64 `json:"qty"`
	TenantID string  `json:"tenant_id,omitempty"`
}

// ─── Registration ────────────────────────────────────────────────────────

// registerBillingRoutes wires the /api/v1/billing/* + Stripe webhook
// endpoints.
func (s *Server) registerBillingRoutes() {
	s.mux.HandleFunc("GET /api/v1/billing/customers", s.handleBillingListCustomers)
	s.mux.HandleFunc("GET /api/v1/billing/customers/{tenantId}", s.handleBillingGetCustomer)
	s.mux.HandleFunc("GET /api/v1/billing/meters", s.handleBillingListMeters)
	s.mux.HandleFunc("POST /api/v1/billing/meter", s.handleBillingMeter)
	s.mux.HandleFunc("POST /api/v1/billing/customers/{tenantId}/portal", s.handleBillingPortalLink)
	// Direct Stripe webhook — see file-level comment for the coordination
	// note with Phase 10.2's /webhooks/in/{slug} catch-all.
	s.mux.HandleFunc("POST /webhooks/in/stripe", s.handleStripeWebhook)
}

func (s *Server) registerBillingOpenAPI() {
	if s.openapi == nil {
		return
	}
	b := s.openapi
	b.AddTag("billing", "Billing adapter + per-tenant usage meters")
	b.Register("GET", "/api/v1/billing/customers", openapi.RouteMeta{
		Summary: "List billing customers (per-tenant)", Tags: []string{"billing"},
	})
	b.Register("GET", "/api/v1/billing/customers/{tenantId}", openapi.RouteMeta{
		Summary: "Fetch a single tenant's billing customer", Tags: []string{"billing"},
		Parameters: []openapi.Parameter{
			{Name: "tenantId", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("GET", "/api/v1/billing/meters", openapi.RouteMeta{
		Summary: "List per-tenant usage meters for the current period",
		Tags:    []string{"billing"},
		Parameters: []openapi.Parameter{
			{Name: "tenant", In: "query", Schema: map[string]any{"type": "string"}},
			{Name: "period_start", In: "query",
				Description: "RFC3339 timestamp; defaults to current month start",
				Schema:      map[string]any{"type": "string"}},
			{Name: "bucket", In: "query",
				Description: "month | day",
				Schema:      map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/api/v1/billing/meter", openapi.RouteMeta{
		Summary: "Record a usage-meter increment for the current period",
		Tags:    []string{"billing"},
	})
	b.Register("POST", "/api/v1/billing/customers/{tenantId}/portal", openapi.RouteMeta{
		Summary: "Mint a short-lived billing customer portal link",
		Tags:    []string{"billing"},
		Parameters: []openapi.Parameter{
			{Name: "tenantId", In: "path", Required: true,
				Schema: map[string]any{"type": "string"}},
		},
	})
	b.Register("POST", "/webhooks/in/stripe", openapi.RouteMeta{
		Summary: "Stripe webhook receiver", Tags: []string{"billing"},
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// billingUnavailable returns true (and writes a 503) when the billing
// service isn't wired. Used by mutating endpoints; the read endpoints
// degrade to empty responses instead.
func (s *Server) billingUnavailable(w http.ResponseWriter) bool {
	if s.billing == nil {
		writeError(w, http.StatusServiceUnavailable,
			"BILLING_NOT_CONFIGURED",
			"billing module is not configured on this runtime", nil)
		return true
	}
	return false
}

// writeBillingError maps billing package errors to HTTP responses.
func writeBillingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	case errors.Is(err, billing.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "billing customer not found", nil)
	case errors.Is(err, billing.ErrBillingUnavailable):
		writeError(w, http.StatusBadGateway, "BILLING_PROVIDER_UNAVAILABLE", err.Error(), nil)
	case errors.Is(err, billing.ErrSignatureInvalid):
		writeError(w, http.StatusBadRequest, "WEBHOOK_SIGNATURE_INVALID", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
	}
}

// ─── GET /api/v1/billing/customers ────────────────────────────────────────

func (s *Server) handleBillingListCustomers(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.billing.list_customers")
	defer span.End()

	resp := billingCustomerListResponse{Customers: []billing.Customer{}, Adapter: "none"}
	if s.billing == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Adapter = s.billing.AdapterName()
	customers, err := s.billing.ListCustomers(ctx)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	if customers != nil {
		resp.Customers = customers
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── GET /api/v1/billing/customers/{tenantId} ─────────────────────────────

func (s *Server) handleBillingGetCustomer(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.billing.get_customer")
	defer span.End()

	tenantID := r.PathValue("tenantId")
	if s.billing == nil {
		// Synthesise a "free plan" row so the dashboard renders the
		// default state without a 503.
		now := time.Now().UTC().Format(time.RFC3339Nano)
		writeJSON(w, http.StatusOK, billing.Customer{
			TenantID:  tenantID,
			Plan:      "free",
			CreatedAt: now,
			UpdatedAt: now,
		})
		return
	}
	cust, err := s.billing.GetCustomer(ctx, tenantID)
	if err != nil {
		if errors.Is(err, billing.ErrNotFound) {
			// Synthesise a free-plan row for tenants that don't yet have a
			// billing customer. The dashboard expects every tenant id to
			// resolve to *some* row.
			now := time.Now().UTC().Format(time.RFC3339Nano)
			writeJSON(w, http.StatusOK, billing.Customer{
				TenantID:  tenantID,
				Plan:      "free",
				CreatedAt: now,
				UpdatedAt: now,
			})
			return
		}
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cust)
}

// ─── GET /api/v1/billing/meters ───────────────────────────────────────────

func (s *Server) handleBillingListMeters(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.billing.list_meters")
	defer span.End()

	resp := usageMeterListResponse{Meters: []billing.UsageMeter{}, TotalCostUSD: 0}
	if s.billing == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	q := r.URL.Query()
	tenant := strings.TrimSpace(q.Get("tenant"))

	// Resolve the bucket (default: month). Mirrors the api.billing.meters
	// "bucket" query param.
	bucket := billing.BucketMonth
	if v := strings.TrimSpace(q.Get("bucket")); v != "" {
		bucket = billing.Bucket(v)
	}

	// period_start defaults to "now in the chosen bucket". Callers can
	// supply an explicit RFC3339 to scope to a historical period.
	now := time.Now()
	if v := strings.TrimSpace(q.Get("period_start")); v != "" {
		if t, err := parseRFC3339(v); err == nil {
			now = t
		} else {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"period_start must be RFC3339", nil)
			return
		}
	}
	start, end := billing.PeriodBoundary(bucket, now)

	meters, err := s.billing.ListMeters(ctx, billing.MeterFilters{
		TenantID:    tenant,
		PeriodStart: start,
		PeriodEnd:   end,
	})
	if err != nil {
		writeBillingError(w, err)
		return
	}
	if meters != nil {
		resp.Meters = meters
	}
	// Compute total_cost_usd in-process so the dashboard doesn't have to.
	for _, m := range resp.Meters {
		if m.CostUSD != nil {
			resp.TotalCostUSD += *m.CostUSD
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ─── POST /api/v1/billing/meter ───────────────────────────────────────────

// handleBillingMeter records a usage-meter increment. App code calls this via
// suite.billing.meter(name, qty); the runtime owns the cost computation. The
// tenant is taken from the request body when present, otherwise from the
// authenticated caller's API key.
func (s *Server) handleBillingMeter(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.billing.meter")
	defer span.End()
	if s.billingUnavailable(w) {
		return
	}

	var in meterRequest
	if r.ContentLength != 0 {
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		if err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "could not read body", nil)
			return
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &in); err != nil {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "body must be valid JSON", nil)
				return
			}
		}
	}

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "name is required", nil)
		return
	}
	if in.Qty < 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "qty must be non-negative", nil)
		return
	}

	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = tenantctx.TenantID(ctx)
	}
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"tenant_id is required (no authenticated tenant in context)", nil)
		return
	}

	if err := s.billing.Meter(ctx, in.Name, in.Qty, tenantID); err != nil {
		writeBillingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── POST /api/v1/billing/customers/{tenantId}/portal ─────────────────────

func (s *Server) handleBillingPortalLink(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.billing.portal_link")
	defer span.End()
	if s.billingUnavailable(w) {
		return
	}
	tenantID := r.PathValue("tenantId")

	// Body is optional — empty body is a valid request (defaults apply).
	var in portalLinkRequest
	if r.ContentLength > 0 {
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		if err == nil && len(body) > 0 {
			_ = json.Unmarshal(body, &in)
		}
	}

	link, err := s.billing.PortalLink(ctx, tenantID, in.ReturnURL)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// ─── POST /webhooks/in/stripe ─────────────────────────────────────────────

// handleStripeWebhook accepts a Stripe webhook payload, verifies it via
// the billing service (which uses the official stripe-go signature
// verifier), and applies the event to the local store.
//
// Sticks to 200/400/500 only:
//   - 200 on success or no-op event (Stripe stops retrying).
//   - 400 on invalid signature (Stripe stops retrying — bad config).
//   - 500 on DB failure (Stripe retries).
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "webhooks.in.stripe")
	defer span.End()

	if s.billing == nil {
		writeError(w, http.StatusServiceUnavailable,
			"BILLING_NOT_CONFIGURED",
			"billing module is not configured on this runtime", nil)
		return
	}

	// Stripe signs the raw bytes; read with a generous cap (Stripe events
	// can be up to ~256 KiB).
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	sig := r.Header.Get("Stripe-Signature")

	if err := s.billing.HandleStripeWebhook(ctx, body, sig); err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}
