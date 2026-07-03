// SPDX-License-Identifier: Apache-2.0

// Package billingcmd implements `af-stack billing`: the agent-facing
// surface for setting up real Stripe billing with the bare minimum human
// involvement.
//
// The design goal is agent-first. An agent drives the whole setup through
// the CLI — define plans, and the runtime provisions the Stripe Product +
// Price automatically (no Stripe dashboard visits, no copy-pasted price
// ids). The one thing an agent can't invent is the Stripe secret key: when
// it's missing, `billing status` prints the operator-dashboard link so the
// human can paste it once (or the agent can set it with `billing set-key`
// if it already has one).
//
// Commands (all need AF_STACK_API_KEY = an operator key):
//
//	af-stack billing status                       # adapter/mode + what's needed next
//	af-stack billing set-key --stripe-secret sk_… [--stripe-webhook whsec_…]
//	af-stack billing plans                        # list the catalog
//	af-stack billing plan set --id pro --name Pro --price 29 \
//	        --budget 25 --entitlement simulations=500 [--default]
//	af-stack billing plan rm <id>
package billingcmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// Run dispatches the `billing` subcommands.
func Run(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usage(stderr)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "status":
		return runStatus(ctx, c, stdout)
	case "set-key":
		return runSetKey(ctx, c, rest, stdout, stderr)
	case "plans":
		return runPlans(ctx, c, stdout)
	case "plan":
		return runPlan(ctx, c, rest, stdout, stderr)
	case "help", "-h", "--help":
		return usage(stdout)
	default:
		fmt.Fprintf(stderr, "billing: unknown subcommand %q\n", sub)
		return usage(stderr)
	}
}

func usage(w io.Writer) error {
	fmt.Fprint(w, `af-stack billing — set up real Stripe billing (agent-first)

Commands:
  status                       Show the billing adapter, mode, and next step
  set-key --stripe-secret KEY  Store the Stripe secret key (+ optional
          [--stripe-webhook WHSEC]   webhook signing secret)
  plans                        List the plan catalog
  plan set --id ID --name N …  Create/update a plan (auto-provisions the
                               Stripe Product + Price in live mode)
  plan rm ID                   Delete a plan

plan set flags:
  --id ID                      plan slug (required), e.g. pro
  --name NAME                  display name (required)
  --price N                    USD/month (0 = free; >0 provisions a Stripe price)
  --budget N                   enforced LLM budget USD/month (omit = none)
  --entitlement k=v            repeatable, e.g. --entitlement simulations=500
  --stripe-price price_…       bind an existing Stripe Price instead of
                               auto-provisioning
  --default                    make this the default plan

Env: AF_STACK_URL (runtime), AF_STACK_API_KEY (operator key),
     AF_STACK_DASHBOARD_URL (for the set-key link; default http://localhost:33000)
`)
	return nil
}

// ─── status ─────────────────────────────────────────────────────────────

type settingsStatus struct {
	Adapter          string `json:"adapter"`
	Mode             string `json:"mode"`
	Source           string `json:"source"`
	SecretKeySet     bool   `json:"secret_key_set"`
	SecretKeyLast4   string `json:"secret_key_last4"`
	KeyMode          string `json:"key_mode"`
	WebhookSecretSet bool   `json:"webhook_secret_set"`
	WebhookPath      string `json:"webhook_path"`
	SettingsWritable bool   `json:"settings_writable"`
}

func runStatus(ctx context.Context, c *client.Client, stdout io.Writer) error {
	var s settingsStatus
	if err := c.Do(ctx, "GET", "/admin/billing/settings", nil, &s); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "adapter:  %s\n", s.Adapter)
	fmt.Fprintf(stdout, "mode:     %s\n", s.Mode)
	fmt.Fprintf(stdout, "key:      %s\n", keyState(s))
	if s.Mode == "real" {
		km := s.KeyMode
		if km == "" {
			km = "unknown"
		}
		// Make live↔test explicit: prices provisioned under one mode's key
		// 404 at checkout under the other, so surfacing this avoids the
		// classic "switched keys, checkout broke" trap.
		fmt.Fprintf(stdout, "stripe:   %s mode\n", km)
		fmt.Fprintln(stdout, "\nStripe is live. `af-stack billing plan set` will auto-provision")
		fmt.Fprintln(stdout, "the Stripe Product + Price for any paid plan.")
		fmt.Fprintln(stdout, "Prices are tied to this key — if you swap keys (e.g. test↔live),")
		fmt.Fprintln(stdout, "saving the new key re-provisions every paid plan automatically.")
		return nil
	}
	// Not live — the one human step: provide a key.
	fmt.Fprintln(stdout, "\nStripe is not live yet — paid checkout runs in dev mode (plans")
	fmt.Fprintln(stdout, "apply instantly, nothing is charged). To enable real checkout, add")
	fmt.Fprintln(stdout, "your Stripe secret key one of two ways:")
	if s.SettingsWritable {
		fmt.Fprintln(stdout, "  • CLI:       af-stack billing set-key --stripe-secret sk_test_…")
		fmt.Fprintf(stdout, "  • Dashboard: %s\n", dashboardBillingURL())
	} else {
		fmt.Fprintln(stdout, "  (this runtime has no DB+KMS, so keys come from env vars:")
		fmt.Fprintln(stdout, "   set STRIPE_SECRET_KEY / STRIPE_WEBHOOK_SECRET and restart)")
	}
	return nil
}

func keyState(s settingsStatus) string {
	if !s.SecretKeySet {
		return "not set"
	}
	src := s.Source
	if src == "" {
		src = "configured"
	}
	return fmt.Sprintf("•••• %s (from %s)", s.SecretKeyLast4, src)
}

func dashboardBillingURL() string {
	base := strings.TrimRight(os.Getenv("AF_STACK_DASHBOARD_URL"), "/")
	if base == "" {
		base = "http://localhost:33000"
	}
	return base + "/platform/billing"
}

// ─── set-key ────────────────────────────────────────────────────────────

func runSetKey(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("billing set-key", flag.ContinueOnError)
	fs.SetOutput(stderr)
	secret := fs.String("stripe-secret", "", "Stripe secret key (sk_test_… / sk_live_…)")
	webhook := fs.String("stripe-webhook", "", "Stripe webhook signing secret (whsec_…)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*secret) == "" && strings.TrimSpace(*webhook) == "" {
		return fmt.Errorf("set-key: pass --stripe-secret and/or --stripe-webhook")
	}
	body := map[string]string{}
	if *secret != "" {
		body["stripe_secret_key"] = strings.TrimSpace(*secret)
	}
	if *webhook != "" {
		body["stripe_webhook_secret"] = strings.TrimSpace(*webhook)
	}
	var s settingsStatus
	if err := c.Do(ctx, "PUT", "/admin/billing/settings", body, &s); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Saved. Billing is now in %s mode (key %s).\n", s.Mode, keyState(s))
	if s.Mode == "real" {
		fmt.Fprintln(stdout, "Define plans with `af-stack billing plan set` — prices provision automatically.")
	}
	return nil
}

// ─── plans ──────────────────────────────────────────────────────────────

type plan struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	StripePriceID *string        `json:"stripe_price_id"`
	PriceUSDMonth float64        `json:"price_usd_month"`
	LLMBudgetUSD  *float64       `json:"llm_budget_usd"`
	Entitlements  map[string]any `json:"entitlements"`
	IsDefault     bool           `json:"is_default"`
}

func runPlans(ctx context.Context, c *client.Client, stdout io.Writer) error {
	var resp struct {
		Plans []plan `json:"plans"`
	}
	if err := c.Do(ctx, "GET", "/billing/plans", nil, &resp); err != nil {
		return err
	}
	if len(resp.Plans) == 0 {
		fmt.Fprintln(stdout, "No plans yet. Create one with `af-stack billing plan set`.")
		return nil
	}
	for _, p := range resp.Plans {
		price := "price:—"
		if p.StripePriceID != nil && *p.StripePriceID != "" {
			price = "price:" + *p.StripePriceID
		} else if p.PriceUSDMonth > 0 {
			price = "price:UNPROVISIONED"
		}
		budget := "budget:none"
		if p.LLMBudgetUSD != nil {
			budget = fmt.Sprintf("budget:$%.2f", *p.LLMBudgetUSD)
		}
		def := ""
		if p.IsDefault {
			def = " [default]"
		}
		fmt.Fprintf(stdout, "%-12s $%-7.2f %-28s %-14s %s%s\n",
			p.ID, p.PriceUSDMonth, price, budget, entitlementsStr(p.Entitlements), def)
	}
	return nil
}

func entitlementsStr(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return "{" + strings.Join(parts, " ") + "}"
}

// ─── plan set / rm ──────────────────────────────────────────────────────

func runPlan(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("billing plan: expected `set` or `rm`")
	}
	switch args[0] {
	case "set":
		return runPlanSet(ctx, c, args[1:], stdout, stderr)
	case "rm", "delete":
		return runPlanRm(ctx, c, args[1:], stdout)
	default:
		return fmt.Errorf("billing plan: unknown action %q (want set|rm)", args[0])
	}
}

// entitlementFlags collects repeated --entitlement k=v pairs.
type entitlementFlags map[string]any

func (e entitlementFlags) String() string { return entitlementsStr(e) }
func (e entitlementFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("entitlement must be key=value, got %q", v)
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return fmt.Errorf("entitlement key is empty in %q", v)
	}
	val = strings.TrimSpace(val)
	// Store numbers as numbers so the JSON entitlements match how apps
	// read them (e.g. simulations: 500, not "500").
	if n, err := strconv.ParseFloat(val, 64); err == nil {
		e[k] = n
	} else if b, err := strconv.ParseBool(val); err == nil {
		e[k] = b
	} else {
		e[k] = val
	}
	return nil
}

func runPlanSet(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("billing plan set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "plan slug (required)")
	name := fs.String("name", "", "display name (required)")
	price := fs.Float64("price", 0, "USD per month")
	budget := fs.Float64("budget", -1, "enforced LLM budget USD/month (omit for none)")
	stripePrice := fs.String("stripe-price", "", "bind an existing Stripe Price id (skip auto-provision)")
	isDefault := fs.Bool("default", false, "make this the default plan")
	ents := entitlementFlags{}
	fs.Var(ents, "entitlement", "entitlement key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*id) == "" || strings.TrimSpace(*name) == "" {
		return fmt.Errorf("plan set: --id and --name are required")
	}

	body := map[string]any{
		"id":              strings.TrimSpace(*id),
		"name":            strings.TrimSpace(*name),
		"price_usd_month": *price,
		"entitlements":    map[string]any(ents),
		"is_default":      *isDefault,
	}
	if *budget >= 0 {
		body["llm_budget_usd"] = *budget
	}
	if strings.TrimSpace(*stripePrice) != "" {
		body["stripe_price_id"] = strings.TrimSpace(*stripePrice)
	}

	var out plan
	if err := c.Do(ctx, "PUT", "/admin/billing/plans", body, &out); err != nil {
		return planSetError(err, *price)
	}
	fmt.Fprintf(stdout, "Saved plan %q.\n", out.ID)
	if out.StripePriceID != nil && *out.StripePriceID != "" {
		fmt.Fprintf(stdout, "Stripe price: %s\n", *out.StripePriceID)
	} else if out.PriceUSDMonth > 0 {
		fmt.Fprintln(stdout, "No Stripe price yet (dev mode) — customers get instant dev checkout.")
		fmt.Fprintf(stdout, "Set a key to enable real checkout: %s\n", dashboardBillingURL())
	}
	return nil
}

// planSetError adds guidance when provisioning failed for a lack of key.
func planSetError(err error, price float64) error {
	msg := err.Error()
	if price > 0 && (strings.Contains(msg, "provision stripe price") || strings.Contains(msg, "stripe")) {
		return fmt.Errorf("%w\n\nThis paid plan needs a live Stripe key to provision its price.\nAdd one with `af-stack billing set-key --stripe-secret sk_test_…`\nor in the dashboard: %s", err, dashboardBillingURL())
	}
	return err
}

func runPlanRm(ctx context.Context, c *client.Client, args []string, stdout io.Writer) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("plan rm: expected a plan id")
	}
	id := strings.TrimSpace(args[0])
	var out struct {
		Deleted string `json:"deleted"`
	}
	if err := c.Do(ctx, "DELETE", "/admin/billing/plans/"+id, nil, &out); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Deleted plan %q.\n", id)
	return nil
}
