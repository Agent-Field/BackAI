// SPDX-License-Identifier: Apache-2.0

// billing_plans.go — turnkey billing: the plans catalog, hosted checkout,
// tenant entitlements, and the operator-panel Stripe settings.
//
// The goal is Clerk-Billing-style DX: an operator pastes Stripe keys into
// Platform → Billing (stored KMS-encrypted, hot-swapped into the live
// client — no restart), binds catalog plans to Stripe Prices, and from
// then on the loop is closed automatically: checkout → Stripe webhook →
// plan applied → enforced LLM budget updated. App code reduces to
// suite.billing.checkout(plan) + suite.billing.entitlements().

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

func (s *Server) registerBillingPlanRoutes() {
	// Public catalog read — pricing pages need it pre-auth.
	s.mux.HandleFunc("GET /api/v1/billing/plans", s.handleBillingListPlans)
	// Tenant-facing purchase + entitlement surface.
	s.mux.HandleFunc("POST /api/v1/billing/checkout", s.handleBillingCheckout)
	s.mux.HandleFunc("GET /api/v1/billing/entitlements", s.handleBillingEntitlements)
	// Operator surface.
	s.mux.HandleFunc("PUT /api/v1/admin/billing/plans", s.handleAdminUpsertPlan)
	s.mux.HandleFunc("DELETE /api/v1/admin/billing/plans/{id}", s.handleAdminDeletePlan)
	s.mux.HandleFunc("GET /api/v1/admin/billing/settings", s.handleAdminGetBillingSettings)
	s.mux.HandleFunc("PUT /api/v1/admin/billing/settings", s.handleAdminPutBillingSettings)
}

// applyPlanHook is wired into billing.Service.SetOnPlanApplied at
// construction: whenever a plan lands on a tenant (Stripe webhook or
// stub checkout) the plan's LLM budget becomes the tenant's enforced
// monthly cap immediately.
func (s *Server) applyPlanHook(ctx context.Context, tenantID string, plan billing.Plan) {
	if s.budgets == nil || plan.LLMBudgetUSD == nil {
		return
	}
	if _, err := s.budgets.Set(ctx, cost.SetBudgetInput{
		TenantID:          tenantID,
		MonthlyUSD:        *plan.LLMBudgetUSD,
		AlertThresholdPct: 80,
	}); err != nil {
		s.log.Warn("billing: apply plan budget failed",
			"tenant", tenantID, "plan", plan.ID, "error", err)
		return
	}
	s.log.Info("billing: plan applied",
		"tenant", tenantID, "plan", plan.ID, "monthly_usd", *plan.LLMBudgetUSD)
}

// ─── Catalog ──────────────────────────────────────────────────────────────

func (s *Server) handleBillingListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.billing.Plans(r.Context())
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) handleAdminUpsertPlan(w http.ResponseWriter, r *http.Request) {
	if s.billingUnavailable(w) {
		return
	}
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminBilling, rbac.ActionWrite) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "request body is required", nil)
		return
	}
	var p billing.Plan
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	out, err := s.billing.UpsertPlan(r.Context(), p)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "billing.plan.upsert",
		ResourceType: "billing_plan",
		ResourceID:   out.ID,
		Metadata: map[string]any{
			"price_usd_month": out.PriceUSDMonth,
			"llm_budget_usd":  out.LLMBudgetUSD,
			"is_default":      out.IsDefault,
		},
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAdminDeletePlan(w http.ResponseWriter, r *http.Request) {
	if s.billingUnavailable(w) {
		return
	}
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminBilling, rbac.ActionWrite) {
		return
	}
	id := r.PathValue("id")
	if err := s.billing.DeletePlan(r.Context(), id); err != nil {
		writeBillingError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "billing.plan.delete",
		ResourceType: "billing_plan",
		ResourceID:   id,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// ─── Checkout + entitlements ──────────────────────────────────────────────

type checkoutRequest struct {
	PlanID     string `json:"plan_id"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url,omitempty"`
	// TenantID is for multi-tenant app backends calling on behalf of a
	// tenant (same convention as POST /billing/meter). When omitted the
	// resolved request tenant is used.
	TenantID string `json:"tenant_id,omitempty"`
}

func (s *Server) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
	if s.billingUnavailable(w) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "request body is required", nil)
		return
	}
	var in checkoutRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		tenantID = tenantctx.TenantID(r.Context())
	}
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"tenant_id is required (body field or tenant-scoped credential)", nil)
		return
	}
	if strings.TrimSpace(in.SuccessURL) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "success_url is required", nil)
		return
	}
	res, err := s.billing.Checkout(r.Context(), tenantID, in.PlanID, in.SuccessURL, in.CancelURL)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{
		Action:       "billing.checkout",
		ResourceType: "billing_plan",
		ResourceID:   in.PlanID,
		Metadata: map[string]any{
			"tenant_id":        tenantID,
			"applied_directly": res.AppliedDirectly,
		},
	})
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBillingEntitlements(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	if tenantID == "" {
		tenantID = tenantctx.TenantID(r.Context())
	}
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"tenant is required (query param or tenant-scoped credential)", nil)
		return
	}
	plan, err := s.billing.PlanFor(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, billing.ErrPlanNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no plan catalog configured", nil)
			return
		}
		writeBillingError(w, err)
		return
	}
	// Current-period meter usage rides along so a single call answers
	// "am I entitled and how much have I used".
	usage := map[string]float64{}
	meters, err := s.billing.ListMeters(r.Context(), billing.MeterFilters{TenantID: tenantID})
	if err == nil {
		for _, m := range meters {
			usage[m.Meter] += m.Quantity
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":    tenantID,
		"plan":         plan,
		"entitlements": plan.Entitlements,
		"usage":        usage,
	})
}

// ─── Operator settings ────────────────────────────────────────────────────

type billingSettingsRequest struct {
	// Pointers so "field absent" (leave unchanged) is distinguishable
	// from "" (clear the stored value).
	StripeSecretKey     *string `json:"stripe_secret_key,omitempty"`
	StripeWebhookSecret *string `json:"stripe_webhook_secret,omitempty"`
}

func (s *Server) billingSettingsStatus(r *http.Request) map[string]any {
	ctx := r.Context()
	secretKey, webhookSecret, fromVault, _ := s.billingSettings.StripeKeys(ctx,
		os.Getenv(billing.EnvSecretKey), os.Getenv(billing.EnvWebhookSecret))
	source := "none"
	if fromVault {
		source = "vault"
	} else if secretKey != "" {
		source = "env"
	}
	last4 := ""
	if n := len(secretKey); n >= 4 {
		last4 = secretKey[n-4:]
	}
	c := s.billing.Client()
	mode := "stub"
	adapter := "none"
	if c != nil {
		adapter = c.AdapterName()
		if !c.IsStub() {
			mode = "real"
		}
	}
	return map[string]any{
		"adapter":            adapter,
		"mode":               mode,
		"source":             source,
		"secret_key_set":     secretKey != "",
		"secret_key_last4":   last4,
		"webhook_secret_set": webhookSecret != "",
		"webhook_path":       "/webhooks/in/stripe",
		"settings_writable":  s.billingSettings != nil,
	}
}

func (s *Server) handleAdminGetBillingSettings(w http.ResponseWriter, r *http.Request) {
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminBilling, rbac.ActionRead) {
		return
	}
	writeJSON(w, http.StatusOK, s.billingSettingsStatus(r))
}

func (s *Server) handleAdminPutBillingSettings(w http.ResponseWriter, r *http.Request) {
	if s.operatorAccessDenied(w, r, rbac.ResourceAdminBilling, rbac.ActionWrite) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<10))
	if err != nil || len(body) == 0 {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "request body is required", nil)
		return
	}
	var in billingSettingsRequest
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return
	}
	ctx := r.Context()
	if in.StripeSecretKey != nil {
		if err := s.billingSettings.Set(ctx, billing.SettingStripeSecretKey, *in.StripeSecretKey); err != nil {
			writeBillingError(w, err)
			return
		}
	}
	if in.StripeWebhookSecret != nil {
		if err := s.billingSettings.Set(ctx, billing.SettingStripeWebhookSecret, *in.StripeWebhookSecret); err != nil {
			writeBillingError(w, err)
			return
		}
	}
	// Hot-swap the live client from the now-effective key material —
	// menu-driven setup takes effect without a restart.
	secretKey, webhookSecret, _, err := s.billingSettings.StripeKeys(ctx,
		os.Getenv(billing.EnvSecretKey), os.Getenv(billing.EnvWebhookSecret))
	if err != nil {
		writeBillingError(w, err)
		return
	}
	s.billing.SwapClient(billing.NewStripeClientFromConfig(secretKey, webhookSecret, s.log))
	s.audit.Write(ctx, r, audit.Event{
		Action:       "billing.settings.update",
		ResourceType: "billing_settings",
		ResourceID:   "stripe",
		Metadata: map[string]any{
			"secret_key_updated":     in.StripeSecretKey != nil,
			"webhook_secret_updated": in.StripeWebhookSecret != nil,
		},
	})
	writeJSON(w, http.StatusOK, s.billingSettingsStatus(r))
}
