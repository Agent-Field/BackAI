// SPDX-License-Identifier: Apache-2.0

// Package remote implements billing.Client by talking the HTTP protocol
// defined in docs/adapters/protocols/billing-v1.md to a sidecar adapter.
//
// One Adapter per AF_STACK_BILLING_ADAPTER_URL. Goroutine-safe; the
// underlying remote.Client owns the connection pool.
package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	stripe "github.com/stripe/stripe-go/v82"
)

// Adapter is a billing.Client backed by a remote sidecar speaking the
// billing-v1 protocol.
type Adapter struct {
	client *remote.Client
	name   string // active adapter name from /v1/capabilities
	isStub bool   // cached from /v1/capabilities

	// CachedAt is the time we last refreshed capabilities. Exposed
	// for tests and the operator-facing /admin/adapters status.
	CachedAt time.Time
}

// Compile-time interface check.
var _ billing.Client = (*Adapter)(nil)

// Config is the env-driven configuration. Use NewFromEnv for the
// canonical wire-up.
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
}

// New returns an Adapter for the given config. Performs a synchronous
// GET /v1/capabilities to verify the sidecar speaks the protocol; if
// that call fails, returns the error so the runtime can refuse to start
// (rather than discovering the problem on the first user request).
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("billing/remote: BaseURL required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 2
	}
	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("billing/remote: client: %w", err)
	}

	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("billing/remote: capabilities probe: %w", err)
	}
	return a, nil
}

// refreshCapabilities pulls /v1/capabilities and decodes the capabilities
// into our cached fields.
func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "billing" {
		return fmt.Errorf("billing/remote: adapter reports slot=%q; expected billing", resp.Slot)
	}
	a.name = resp.Name
	var caps billingCaps
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &caps); err != nil {
			return fmt.Errorf("billing/remote: decode capabilities: %w", err)
		}
	}
	a.isStub = caps.IsStub
	a.CachedAt = time.Now()
	return nil
}

// AdapterName returns the active adapter's name (from /v1/capabilities).
func (a *Adapter) AdapterName() string {
	if a == nil {
		return ""
	}
	return a.name
}

// IsStub reports whether this client is in stub mode.
func (a *Adapter) IsStub() bool {
	if a == nil {
		return false
	}
	return a.isStub
}

// CreateCustomer implements billing.Client by POST /v1/customers.
func (a *Adapter) CreateCustomer(ctx context.Context, tenantID, email string) (string, error) {
	body := createCustomerRequest{
		TenantID: tenantID,
		Email:    email,
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPost,
		Path:     "/v1/customers",
		Body:     body,
		TenantID: tenantID,
	})
	if err != nil {
		if remote.IsCode(err, "provider_unavailable") || remote.IsCode(err, "upstream_error") {
			return "", fmt.Errorf("%w: create customer: %v", billing.ErrBillingUnavailable, err)
		}
		return "", fmt.Errorf("billing/remote: create customer: %w", err)
	}
	var wire customerWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return "", fmt.Errorf("billing/remote: decode customer: %w", err)
	}
	return wire.ID, nil
}

// GetCustomer implements billing.Client by GET /v1/customers/{id}.
func (a *Adapter) GetCustomer(ctx context.Context, id string) (billing.Customer, error) {
	if id == "" {
		return billing.Customer{}, fmt.Errorf("%w: customer id required", billing.ErrInvalidInput)
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodGet,
		Path:   "/v1/customers/" + id,
	})
	if err != nil {
		if remote.IsCode(err, "customer_not_found") {
			return billing.Customer{}, fmt.Errorf("%w: customer id %q not found", billing.ErrNotFound, id)
		}
		if remote.IsCode(err, "provider_unavailable") || remote.IsCode(err, "upstream_error") {
			return billing.Customer{}, fmt.Errorf("%w: get customer: %v", billing.ErrBillingUnavailable, err)
		}
		return billing.Customer{}, fmt.Errorf("billing/remote: get customer: %w", err)
	}
	var wire customerWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return billing.Customer{}, fmt.Errorf("billing/remote: decode customer: %w", err)
	}
	return wireToCustomer(wire), nil
}

// CreatePortalLink implements billing.Client by POST /v1/customers/{id}/portal.
func (a *Adapter) CreatePortalLink(ctx context.Context, customerID, returnURL string) (billing.PortalLink, error) {
	if customerID == "" {
		return billing.PortalLink{}, fmt.Errorf("%w: customer id required", billing.ErrInvalidInput)
	}
	body := portalLinkRequest{
		ReturnURL: returnURL,
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/customers/" + customerID + "/portal",
		Body:   body,
	})
	if err != nil {
		if remote.IsCode(err, "provider_unavailable") || remote.IsCode(err, "upstream_error") {
			return billing.PortalLink{}, fmt.Errorf("%w: create portal link: %v", billing.ErrBillingUnavailable, err)
		}
		return billing.PortalLink{}, fmt.Errorf("billing/remote: create portal link: %w", err)
	}
	var wire portalLinkWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return billing.PortalLink{}, fmt.Errorf("billing/remote: decode portal link: %w", err)
	}
	return billing.PortalLink{
		URL:       wire.URL,
		ExpiresAt: wire.ExpiresAt,
	}, nil
}

// VerifyWebhook implements billing.Client by POST /v1/webhooks/verify.
func (a *Adapter) VerifyWebhook(body []byte, sigHeader string) (*stripe.Event, error) {
	// Encode body as base64 per the protocol.
	bodyB64 := base64.StdEncoding.EncodeToString(body)
	req := webhookVerifyRequest{
		BodyBase64:      bodyB64,
		SignatureHeader: sigHeader,
	}
	resp, err := a.client.Do(context.Background(), remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/webhooks/verify",
		Body:   req,
	})
	if err != nil {
		if remote.IsCode(err, "invalid_signature") {
			return nil, fmt.Errorf("%w: signature verification failed", billing.ErrSignatureInvalid)
		}
		if remote.IsCode(err, "provider_unavailable") || remote.IsCode(err, "upstream_error") {
			return nil, fmt.Errorf("%w: webhook verify: %v", billing.ErrBillingUnavailable, err)
		}
		return nil, fmt.Errorf("billing/remote: webhook verify: %w", err)
	}
	var wire webhookVerifyResponse
	if err := resp.DecodeJSON(&wire); err != nil {
		return nil, fmt.Errorf("billing/remote: decode webhook verify: %w", err)
	}
	if !wire.Verified {
		return nil, fmt.Errorf("%w: adapter returned verified=false", billing.ErrSignatureInvalid)
	}

	// Unmarshal the decoded event into a *stripe.Event.
	ev := &stripe.Event{}
	if err := json.Unmarshal(wire.Decoded, ev); err != nil {
		return nil, fmt.Errorf("billing/remote: unmarshal stripe event: %w", err)
	}
	return ev, nil
}

// --- wire shapes ---------------------------------------------------------

type billingCaps struct {
	SupportsCustomers           bool   `json:"supports_customers"`
	SupportsSubscriptions       bool   `json:"supports_subscriptions"`
	SupportsMeteredBilling      bool   `json:"supports_metered_billing"`
	SupportsCustomerPortal      bool   `json:"supports_customer_portal"`
	SupportsUsageReporting      bool   `json:"supports_usage_reporting"`
	SupportsWebhookVerification bool   `json:"supports_webhook_verification"`
	SupportsDisputes            bool   `json:"supports_disputes"`
	SupportsRefunds             bool   `json:"supports_refunds"`
	DefaultCurrency             string `json:"default_currency"`
	IsStub                      bool   `json:"is_stub"`
	AdminDashboardURL           string `json:"admin_dashboard_url"`
}

type createCustomerRequest struct {
	TenantID string            `json:"tenant_id"`
	Email    string            `json:"email"`
	Name     string            `json:"name,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type customerWire struct {
	ID                 string  `json:"id"`
	TenantID           string  `json:"tenant_id"`
	Email              string  `json:"email"`
	Plan               string  `json:"plan"`
	TrialEndsAt        *string `json:"trial_ends_at"`
	CurrentPeriodEnd   *string `json:"current_period_end"`
	SubscriptionStatus *string `json:"subscription_status"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func wireToCustomer(w customerWire) billing.Customer {
	c := billing.Customer{
		TenantID:           w.TenantID,
		Plan:               w.Plan,
		TrialEndsAt:        w.TrialEndsAt,
		CurrentPeriodEnd:   w.CurrentPeriodEnd,
		SubscriptionStatus: w.SubscriptionStatus,
		CreatedAt:          w.CreatedAt,
		UpdatedAt:          w.UpdatedAt,
	}
	if w.Email != "" {
		c.Email = &w.Email
	}
	if w.ID != "" {
		c.StripeCustomerID = &w.ID
	}
	return c
}

type portalLinkRequest struct {
	ReturnURL string `json:"return_url,omitempty"`
}

type portalLinkWire struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

type webhookVerifyRequest struct {
	BodyBase64      string `json:"body_base64"`
	SignatureHeader string `json:"signature_header"`
}

type webhookVerifyResponse struct {
	Verified  bool            `json:"verified"`
	EventType string          `json:"event_type"`
	EventID   string          `json:"event_id"`
	Decoded   json.RawMessage `json:"decoded"`
}
