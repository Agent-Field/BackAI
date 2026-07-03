// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

const (
	// EnvLagoAPIURL points at the Lago API root, for example
	// http://lago-api:3000 or https://api.getlago.com.
	EnvLagoAPIURL = "LAGO_API_URL"
	// EnvLagoAPIKey is the bearer token used for Lago's API.
	EnvLagoAPIKey = "LAGO_API_KEY"
)

// NewLagoClientFromEnv returns a Lago adapter. Missing URL or API key
// intentionally falls back to a deterministic stub so local AF Stack
// development still renders the billing page.
func NewLagoClientFromEnv(log *slog.Logger) Client {
	if log == nil {
		log = slog.Default()
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv(EnvLagoAPIURL)), "/")
	apiKey := strings.TrimSpace(os.Getenv(EnvLagoAPIKey))
	if baseURL == "" || apiKey == "" {
		log.Info("billing: adapter running in stub mode", "adapter", "lago", "reason", "LAGO_API_URL or LAGO_API_KEY unset")
		return &stubLagoClient{}
	}
	log.Info("billing: adapter configured", "adapter", "lago", "url", baseURL)
	return &realLagoClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type realLagoClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func (c *realLagoClient) AdapterName() string { return "lago" }
func (c *realLagoClient) IsStub() bool        { return false }

func (c *realLagoClient) CreateCustomer(ctx context.Context, tenantID, email string) (string, error) {
	externalID := strings.TrimSpace(tenantID)
	if externalID == "" {
		return "", fmt.Errorf("%w: tenant_id is required", ErrInvalidInput)
	}
	body := map[string]any{
		"customer": map[string]any{
			"external_id": externalID,
			"email":       strings.TrimSpace(email),
		},
	}
	var out lagoCustomerEnvelope
	status, raw, err := c.doJSON(ctx, http.MethodPost, "/api/v1/customers", body, &out)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
		// If Lago already has this external_id but the local snapshot is
		// missing it, the portal endpoint can still resolve by tenantID.
		return externalID, nil
	}
	if status >= 400 {
		return "", lagoHTTPError(status, raw, "create customer")
	}
	if out.Customer.ExternalID != "" {
		return out.Customer.ExternalID, nil
	}
	return externalID, nil
}

func (c *realLagoClient) GetCustomer(ctx context.Context, id string) (Customer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Customer{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	var out lagoCustomerEnvelope
	status, raw, err := c.doJSON(ctx, http.MethodGet, "/api/v1/customers/"+url.PathEscape(id), nil, &out)
	if err != nil {
		return Customer{}, err
	}
	if status == http.StatusNotFound {
		return Customer{}, ErrNotFound
	}
	if status >= 400 {
		return Customer{}, lagoHTTPError(status, raw, "get customer")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	externalID := out.Customer.ExternalID
	if externalID == "" {
		externalID = id
	}
	custID := externalID
	cust := Customer{
		StripeCustomerID: &custID,
		Plan:             "free",
		CreatedAt:        firstNonEmpty(out.Customer.CreatedAt, now),
		UpdatedAt:        firstNonEmpty(out.Customer.UpdatedAt, now),
	}
	if out.Customer.Email != "" {
		email := out.Customer.Email
		cust.Email = &email
	}
	return cust, nil
}

func (c *realLagoClient) CreateCheckoutSession(_ context.Context, _, _, _, _, _ string) (string, error) {
	// Lago has no hosted checkout equivalent; operators drive plan
	// changes via the portal / API.
	return "", fmt.Errorf("%w: lago does not support hosted checkout", ErrBillingUnavailable)
}

func (c *realLagoClient) CreatePortalLink(ctx context.Context, customerID, _ string) (PortalLink, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return PortalLink{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	var out lagoPortalEnvelope
	status, raw, err := c.doJSON(ctx, http.MethodGet, "/api/v1/customers/"+url.PathEscape(customerID)+"/portal_url", nil, &out)
	if err != nil {
		return PortalLink{}, err
	}
	if status == http.StatusNotFound {
		return PortalLink{}, ErrNotFound
	}
	if status >= 400 {
		return PortalLink{}, lagoHTTPError(status, raw, "create portal link")
	}
	if strings.TrimSpace(out.Customer.PortalURL) == "" {
		return PortalLink{}, fmt.Errorf("%w: lago portal_url missing", ErrBillingUnavailable)
	}
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	return PortalLink{URL: out.Customer.PortalURL, ExpiresAt: expires}, nil
}

func (c *realLagoClient) VerifyWebhook(_ []byte, _ string) (*stripe.Event, error) {
	return nil, fmt.Errorf("%w: lago webhooks are not handled by the stripe route", ErrInvalidInput)
}

func (c *realLagoClient) doJSON(ctx context.Context, method, path string, in any, out any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, fmt.Errorf("%w: encode lago request: %v", ErrInvalidInput, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: lago request: %v", ErrBillingUnavailable, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 400 && out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, raw, fmt.Errorf("%w: decode lago response: %v", ErrBillingUnavailable, err)
		}
	}
	return resp.StatusCode, raw, nil
}

type lagoCustomerEnvelope struct {
	Customer struct {
		ExternalID string `json:"external_id"`
		Email      string `json:"email"`
		CreatedAt  string `json:"created_at"`
		UpdatedAt  string `json:"updated_at"`
	} `json:"customer"`
}

type lagoPortalEnvelope struct {
	Customer struct {
		PortalURL string `json:"portal_url"`
	} `json:"customer"`
}

func lagoHTTPError(status int, body []byte, op string) error {
	return fmt.Errorf("%w: lago %s returned %d: %s", ErrBillingUnavailable, op, status, previewProviderBody(body))
}

func previewProviderBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 256 {
		return s[:256] + "..."
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type stubLagoClient struct{}

func (c *stubLagoClient) AdapterName() string { return "lago" }
func (c *stubLagoClient) IsStub() bool        { return true }

func (c *stubLagoClient) CreateCustomer(_ context.Context, tenantID, email string) (string, error) {
	suffix := strings.TrimSpace(tenantID)
	if suffix == "" {
		suffix = email
	}
	if suffix == "" {
		suffix = "anon"
	}
	return "lago_stub_" + sanitiseStubID(suffix), nil
}

func (c *stubLagoClient) GetCustomer(_ context.Context, id string) (Customer, error) {
	if strings.TrimSpace(id) == "" {
		return Customer{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	externalID := id
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Customer{
		StripeCustomerID: &externalID,
		Plan:             "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (c *stubLagoClient) CreateCheckoutSession(_ context.Context, _, _, _, _, _ string) (string, error) {
	return "", fmt.Errorf("%w: lago does not support hosted checkout", ErrBillingUnavailable)
}

func (c *stubLagoClient) CreatePortalLink(_ context.Context, customerID, returnURL string) (PortalLink, error) {
	if strings.TrimSpace(customerID) == "" {
		return PortalLink{}, fmt.Errorf("%w: customer id is required", ErrInvalidInput)
	}
	u := "https://example.com/lago-portal-stub?customer=" + sanitiseStubID(customerID)
	if returnURL != "" {
		u += "&return_url=" + sanitiseStubID(returnURL)
	}
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)
	return PortalLink{URL: u, ExpiresAt: expires}, nil
}

func (c *stubLagoClient) VerifyWebhook(_ []byte, _ string) (*stripe.Event, error) {
	return nil, errors.New("billing: stub lago client cannot verify webhooks")
}
