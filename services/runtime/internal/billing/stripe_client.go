// SPDX-License-Identifier: Apache-2.0

// stripe_client.go — thin wrapper around the official stripe-go SDK.
//
// Two modes:
//
//   - Real:  STRIPE_SECRET_KEY is set. Calls hit the Stripe API.
//   - Stub:  STRIPE_SECRET_KEY is unset. Calls return deterministic
//     placeholder values so the dashboard renders something
//     useful in dev / CI without a real Stripe account.
//
// The wrapper deliberately exposes a small surface — CreateCustomer /
// GetCustomer / CreatePortalLink — because the webhook handler does the
// rest of the integration in-band via constructed events. Adding methods
// here should be a deliberate decision (every new method = another
// thing to stub).
package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	stripeportal "github.com/stripe/stripe-go/v82/billingportal/session"
	stripecustomer "github.com/stripe/stripe-go/v82/customer"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

// Client is the provider-neutral interface the Service depends on.
// Implemented by Stripe and Lago adapters. Keeping it small means tests
// can stub it without a full provider mock.
type Client interface {
	// AdapterName returns "stripe", "lago", or a stub variant. It is
	// surfaced to the dashboard so operators can see which provider is
	// active.
	AdapterName() string
	// CreateCustomer returns the external provider customer id. tenantID
	// is passed so adapters like Lago can use it as external_id.
	CreateCustomer(ctx context.Context, tenantID, email string) (string, error)
	// GetCustomer fetches the customer by id. Returns a minimally-populated
	// Customer (TenantID is left empty — the caller fills it in from the
	// metadata it stored on creation).
	GetCustomer(ctx context.Context, id string) (Customer, error)
	// CreatePortalLink mints a short-lived Stripe Customer Portal URL.
	CreatePortalLink(ctx context.Context, customerID, returnURL string) (PortalLink, error)
	// VerifyWebhook validates the Stripe-Signature header against body
	// + the configured webhook secret. Returns the decoded event JSON
	// (as a *stripe.Event) on success.
	VerifyWebhook(body []byte, sigHeader string) (*stripe.Event, error)
	// IsStub reports whether this client is the no-key dev stub. Used
	// by the REST surface to mark stub-only behaviour in responses.
	IsStub() bool
}

// EnvBillingAdapter selects the external billing provider.
const EnvBillingAdapter = "AF_STACK_BILLING_ADAPTER"

// EnvSecretKey is the env var that activates the real Stripe client.
const EnvSecretKey = "STRIPE_SECRET_KEY"

// EnvWebhookSecret is the env var the webhook handler uses to validate
// incoming Stripe-Signature headers.
const EnvWebhookSecret = "STRIPE_WEBHOOK_SECRET"

// NewClientFromEnv returns the configured Client.
//
// AF_STACK_BILLING_ADAPTER selects the provider:
//   - stripe (default): official stripe-go SDK, stubbed when
//     STRIPE_SECRET_KEY is unset.
//   - lago: Lago HTTP API, stubbed when LAGO_API_URL or LAGO_API_KEY is
//     unset.
//   - none: nil client; reads still work from local store, portal
//     mutations return provider unavailable.
//
// log defaults to slog.Default() when nil.
func NewClientFromEnv(log *slog.Logger) Client {
	if log == nil {
		log = slog.Default()
	}
	adapter := strings.ToLower(strings.TrimSpace(os.Getenv(EnvBillingAdapter)))
	if adapter == "" {
		adapter = "stripe"
	}
	switch adapter {
	case "none", "off", "disabled":
		log.Info("billing: external adapter disabled", "adapter", "none")
		return nil
	case "lago":
		return NewLagoClientFromEnv(log)
	case "stripe":
	default:
		log.Warn("billing: unknown adapter; falling back to stripe", "adapter", adapter)
	}
	if key := os.Getenv(EnvSecretKey); key != "" {
		stripe.Key = key
		log.Info("billing: adapter configured", "adapter", "stripe", "mode", "real")
		return &realStripeClient{
			webhookSecret: os.Getenv(EnvWebhookSecret),
			log:           log,
		}
	}
	log.Info("billing: adapter running in stub mode", "adapter", "stripe", "reason", EnvSecretKey+" unset")
	return &stubStripeClient{log: log}
}

// ─── Real client ──────────────────────────────────────────────────────────

type realStripeClient struct {
	webhookSecret string
	log           *slog.Logger
}

func (c *realStripeClient) AdapterName() string { return "stripe" }
func (c *realStripeClient) IsStub() bool        { return false }

func (c *realStripeClient) CreateCustomer(_ context.Context, _ string, email string) (string, error) {
	params := &stripe.CustomerParams{}
	if email != "" {
		params.Email = stripe.String(email)
	}
	cust, err := stripecustomer.New(params)
	if err != nil {
		return "", fmt.Errorf("%w: create customer: %v", ErrStripeUnavailable, err)
	}
	return cust.ID, nil
}

func (c *realStripeClient) GetCustomer(_ context.Context, id string) (Customer, error) {
	if id == "" {
		return Customer{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	cust, err := stripecustomer.Get(id, nil)
	if err != nil {
		return Customer{}, fmt.Errorf("%w: get customer: %v", ErrStripeUnavailable, err)
	}
	stripeID := cust.ID
	out := Customer{
		StripeCustomerID: &stripeID,
		Plan:             "free", // unchanged until a subscription webhook arrives
		CreatedAt:        time.Unix(cust.Created, 0).UTC().Format(time.RFC3339Nano),
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	if cust.Email != "" {
		e := cust.Email
		out.Email = &e
	}
	return out, nil
}

func (c *realStripeClient) CreatePortalLink(_ context.Context, customerID, returnURL string) (PortalLink, error) {
	if customerID == "" {
		return PortalLink{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	params := &stripe.BillingPortalSessionParams{
		Customer: stripe.String(customerID),
	}
	if returnURL != "" {
		params.ReturnURL = stripe.String(returnURL)
	}
	sess, err := stripeportal.New(params)
	if err != nil {
		return PortalLink{}, fmt.Errorf("%w: create portal: %v", ErrStripeUnavailable, err)
	}
	// Stripe Customer Portal links are short-lived. Stripe doesn't
	// expose the exact ttl in the response, but historically 24h is
	// the documented bound — we render that to the dashboard.
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	return PortalLink{URL: sess.URL, ExpiresAt: expires}, nil
}

func (c *realStripeClient) VerifyWebhook(body []byte, sigHeader string) (*stripe.Event, error) {
	if c.webhookSecret == "" {
		return nil, fmt.Errorf("%w: %s unset", ErrSignatureInvalid, EnvWebhookSecret)
	}
	ev, err := stripewebhook.ConstructEvent(body, sigHeader, c.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
	}
	return &ev, nil
}

// ─── Stub client ──────────────────────────────────────────────────────────

// stubStripeClient produces deterministic placeholder values so dev /
// CI can run end-to-end without a Stripe account. It deliberately does
// NOT touch the network — every method is pure.
//
// Stub IDs are prefixed with "cus_stub_" / "bps_stub_" so logs make it
// obvious the runtime is in stub mode.
type stubStripeClient struct {
	log *slog.Logger
}

func (c *stubStripeClient) AdapterName() string { return "stripe" }
func (c *stubStripeClient) IsStub() bool        { return true }

func (c *stubStripeClient) CreateCustomer(_ context.Context, _ string, email string) (string, error) {
	// Use the email (or a fixed sentinel) as the stable suffix so the
	// same tenant always lands on the same stub id within a process.
	suffix := email
	if suffix == "" {
		suffix = "anon"
	}
	return "cus_stub_" + sanitiseStubID(suffix), nil
}

func (c *stubStripeClient) GetCustomer(_ context.Context, id string) (Customer, error) {
	if id == "" {
		return Customer{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	stripeID := id
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Customer{
		StripeCustomerID: &stripeID,
		Plan:             "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (c *stubStripeClient) CreatePortalLink(_ context.Context, customerID, returnURL string) (PortalLink, error) {
	if customerID == "" {
		return PortalLink{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	url := "https://example.com/portal-stub?customer=" + sanitiseStubID(customerID)
	if returnURL != "" {
		url += "&return_url=" + sanitiseStubID(returnURL)
	}
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	return PortalLink{URL: url, ExpiresAt: expires}, nil
}

func (c *stubStripeClient) VerifyWebhook(_ []byte, _ string) (*stripe.Event, error) {
	// In stub mode we don't have a webhook secret so we cannot verify
	// signatures. The webhook handler should branch on IsStub() and
	// either reject or accept-without-verification per its policy.
	return nil, errors.New("billing: stub client cannot verify webhooks")
}

// sanitiseStubID strips characters that would break the stub URL when
// the input is something like an email or a return URL. Only ASCII
// alphanumeric + a few separators survive.
func sanitiseStubID(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b >= '0' && b <= '9',
			b >= 'a' && b <= 'z',
			b >= 'A' && b <= 'Z',
			b == '-', b == '_', b == '.':
			out = append(out, b)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
