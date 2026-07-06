// SPDX-License-Identifier: Apache-2.0

// Package sms implements the notifications.Adapter contract against the
// Twilio Programmable Messaging API (https://www.twilio.com/docs/sms).
//
// Each Send() POSTs a form-encoded body to
// https://api.twilio.com/2010-04-01/Accounts/{SID}/Messages.json with
// HTTP Basic auth (AccountSID:AuthToken). The recipient phone number is
// taken from Notification.To; the message body is data["body"] or the
// template name. Twilio returns a message SID which becomes the row's
// provider_message_id.
//
// Configuration
//
//	AccountSID  required; the Twilio account SID (Basic-auth username).
//	AuthToken   required; the Twilio auth token (Basic-auth password).
//	FromNumber  required; a Twilio-owned "From" phone number (E.164).
package sms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

// defaultBaseURL is Twilio's production API root.
const defaultBaseURL = "https://api.twilio.com"

// httpTimeout caps the per-request HTTP timeout.
const httpTimeout = 30 * time.Second

// ErrCredentialsMissing is returned by New when required Twilio
// credentials are absent.
var ErrCredentialsMissing = errors.New("notifications/sms: account sid, auth token and from number are required")

// Config holds adapter settings.
type Config struct {
	// AccountSID is the Twilio account SID, used as the Basic-auth
	// username. Required.
	AccountSID string

	// AuthToken is the Twilio auth token, used as the Basic-auth
	// password. Required.
	AuthToken string

	// FromNumber is the Twilio-owned sender number (E.164). Required.
	FromNumber string

	// BaseURL overrides the API endpoint (mainly for tests). Defaults to
	// defaultBaseURL.
	BaseURL string

	// HTTPClient lets tests inject a mocked transport. nil -> a client
	// with httpTimeout.
	HTTPClient *http.Client
}

// Adapter implements notifications.Adapter via Twilio.
type Adapter struct {
	accountSID string
	authToken  string
	fromNumber string
	baseURL    string
	client     *http.Client
}

// compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

// New constructs the adapter. Returns ErrCredentialsMissing when any
// required credential is empty so callers fail fast at startup.
func New(_ context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.AccountSID) == "" ||
		strings.TrimSpace(cfg.AuthToken) == "" ||
		strings.TrimSpace(cfg.FromNumber) == "" {
		return nil, ErrCredentialsMissing
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Adapter{
		accountSID: strings.TrimSpace(cfg.AccountSID),
		authToken:  strings.TrimSpace(cfg.AuthToken),
		fromNumber: strings.TrimSpace(cfg.FromNumber),
		baseURL:    baseURL,
		client:     client,
	}, nil
}

// Name returns the stable identifier persisted in the row.
func (a *Adapter) Name() string { return "twilio" }

// Configured reports whether the adapter has full Twilio credentials.
func (a *Adapter) Configured() bool {
	return a != nil && a.accountSID != "" && a.authToken != "" && a.fromNumber != ""
}

// twilioResponse captures the fields we read from Twilio's Messages
// response (both success and error shapes).
type twilioResponse struct {
	SID     string `json:"sid"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// Send dispatches an SMS through Twilio's Messages API. The recipient is
// Notification.To (an E.164 phone number).
func (a *Adapter) Send(ctx context.Context, n notifications.Notification) (string, error) {
	to := strings.TrimSpace(n.To)
	if to == "" {
		return "", fmt.Errorf("%w: to (recipient phone number) required", notifications.ErrInvalidInput)
	}
	body := messageBody(n)
	if body == "" {
		return "", fmt.Errorf("%w: empty message body", notifications.ErrInvalidInput)
	}

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", a.fromNumber)
	form.Set("Body", body)

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		a.baseURL, url.PathEscape(a.accountSID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("notifications/sms: new request: %w", err)
	}
	httpReq.SetBasicAuth(a.accountSID, a.authToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("notifications/sms: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var decoded twilioResponse
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &decoded)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := decoded.Message
		if detail == "" {
			detail = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("notifications/sms: status %d: %s", resp.StatusCode, detail)
	}
	if decoded.SID == "" {
		return "", fmt.Errorf("%w: twilio returned no message sid", notifications.ErrAdapter)
	}
	return decoded.SID, nil
}

// messageBody resolves the SMS text: data["body"] when present, else the
// template name (matching the resend adapter's rendering fallback).
func messageBody(n notifications.Notification) string {
	body, _ := n.Data["body"].(string)
	body = strings.TrimSpace(body)
	if body == "" {
		body = strings.TrimSpace(n.Template)
	}
	return body
}
