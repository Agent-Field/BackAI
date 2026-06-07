// stripe_client.go — thin wrapper around the official stripe-go SDK.
//
// Two modes:
//
//   - Real:  STRIPE_SECRET_KEY is set. Calls hit the Stripe API.
//   - Stub:  STRIPE_SECRET_KEY is unset. Calls return deterministic
//            placeholder values so the dashboard renders something
//            useful in dev / CI without a real Stripe account.
//
// The wrapper deliberately exposes a small surface — CreateCustomer /
// GetCustomer / CreatePortalLink — because the webhook handler does the
// rest of the integration in-band via constructed events. Adding methods
// here should be a deliberate decision (every new method = another
// thing to stub).
package billing

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
	stripecustomer "github.com/stripe/stripe-go/v82/customer"
	stripeportal "github.com/stripe/stripe-go/v82/billingportal/session"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

// Client is the interface the Service depends on. Implemented by
// realStripeClient (when STRIPE_SECRET_KEY is set) and stubStripeClient
// (when unset). Keeping it small means tests can stub it without a
// full Stripe mock.
type Client interface {
	// CreateCustomer returns the new Stripe customer id (e.g. "cus_...").
	CreateCustomer(email string) (string, error)
	// GetCustomer fetches the customer by id. Returns a minimally-populated
	// Customer (TenantID is left empty — the caller fills it in from the
	// metadata it stored on creation).
	GetCustomer(id string) (Customer, error)
	// CreatePortalLink mints a short-lived Stripe Customer Portal URL.
	CreatePortalLink(customerID, returnURL string) (PortalLink, error)
	// VerifyWebhook validates the Stripe-Signature header against body
	// + the configured webhook secret. Returns the decoded event JSON
	// (as a *stripe.Event) on success.
	VerifyWebhook(body []byte, sigHeader string) (*stripe.Event, error)
	// IsStub reports whether this client is the no-key dev stub. Used
	// by the REST surface to mark stub-only behaviour in responses.
	IsStub() bool
}

// EnvSecretKey is the env var that activates the real Stripe client.
const EnvSecretKey = "STRIPE_SECRET_KEY"

// EnvWebhookSecret is the env var the webhook handler uses to validate
// incoming Stripe-Signature headers.
const EnvWebhookSecret = "STRIPE_WEBHOOK_SECRET"

// NewClientFromEnv returns the configured Client.
//
// When STRIPE_SECRET_KEY is set, it returns the real client (with
// stripe.Key set globally — stripe-go's package-level convention).
// When unset, it returns a stub that produces deterministic output.
//
// log defaults to slog.Default() when nil.
func NewClientFromEnv(log *slog.Logger) Client {
	if log == nil {
		log = slog.Default()
	}
	if key := os.Getenv(EnvSecretKey); key != "" {
		stripe.Key = key
		log.Info("billing: stripe client configured", "mode", "real")
		return &realStripeClient{
			webhookSecret: os.Getenv(EnvWebhookSecret),
			log:           log,
		}
	}
	log.Info("billing: stripe client running in stub mode (STRIPE_SECRET_KEY unset)")
	return &stubStripeClient{log: log}
}

// ─── Real client ──────────────────────────────────────────────────────────

type realStripeClient struct {
	webhookSecret string
	log           *slog.Logger
}

func (c *realStripeClient) IsStub() bool { return false }

func (c *realStripeClient) CreateCustomer(email string) (string, error) {
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

func (c *realStripeClient) GetCustomer(id string) (Customer, error) {
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

func (c *realStripeClient) CreatePortalLink(customerID, returnURL string) (PortalLink, error) {
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

func (c *stubStripeClient) IsStub() bool { return true }

func (c *stubStripeClient) CreateCustomer(email string) (string, error) {
	// Use the email (or a fixed sentinel) as the stable suffix so the
	// same tenant always lands on the same stub id within a process.
	suffix := email
	if suffix == "" {
		suffix = "anon"
	}
	return "cus_stub_" + sanitiseStubID(suffix), nil
}

func (c *stubStripeClient) GetCustomer(id string) (Customer, error) {
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

func (c *stubStripeClient) CreatePortalLink(customerID, returnURL string) (PortalLink, error) {
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
