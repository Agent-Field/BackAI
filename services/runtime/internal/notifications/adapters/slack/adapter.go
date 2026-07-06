// SPDX-License-Identifier: Apache-2.0

// Package slack implements the notifications.Adapter contract against a
// Slack incoming webhook (https://api.slack.com/messaging/webhooks).
//
// Each Send() POSTs a JSON body {"text": ...} to the configured webhook
// URL. The notification's Subject and body (data["body"] or the template
// name) are folded into a single Slack message text. Slack incoming
// webhooks return a plain "ok" with no message identifier, so Send
// returns an empty provider message id on success.
//
// Configuration
//
//	WebhookURL  required; the full https://hooks.slack.com/services/...
//	            URL minted from a Slack app's "Incoming Webhooks" feature.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

// httpTimeout caps the per-request HTTP timeout.
const httpTimeout = 30 * time.Second

// ErrWebhookMissing is returned by New when no webhook URL is configured.
var ErrWebhookMissing = errors.New("notifications/slack: webhook url required (set the Slack incoming-webhook URL)")

// Config holds adapter settings.
type Config struct {
	// WebhookURL is the Slack incoming-webhook URL. Required.
	WebhookURL string

	// HTTPClient lets tests inject a mocked transport. nil -> a client
	// with httpTimeout.
	HTTPClient *http.Client
}

// Adapter implements notifications.Adapter via a Slack incoming webhook.
type Adapter struct {
	webhookURL string
	client     *http.Client
}

// compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

// New constructs the adapter. Returns ErrWebhookMissing when WebhookURL is
// empty so callers fail fast at startup rather than at first Send.
func New(_ context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.WebhookURL) == "" {
		return nil, ErrWebhookMissing
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Adapter{
		webhookURL: strings.TrimSpace(cfg.WebhookURL),
		client:     client,
	}, nil
}

// Name returns the stable identifier persisted in the row.
func (a *Adapter) Name() string { return "slack" }

// Configured reports whether the adapter has a webhook URL. Always true
// for an adapter built by New (which rejects an empty URL), but exposed
// for the operator status endpoints and select-by-config wiring.
func (a *Adapter) Configured() bool {
	return a != nil && a.webhookURL != ""
}

// slackRequest mirrors the minimal Slack incoming-webhook body.
type slackRequest struct {
	Text string `json:"text"`
}

// Send posts the notification to the Slack incoming webhook.
//
// Slack incoming webhooks accept any kind, so this adapter does not
// filter on Notification.Kind. Non-2xx responses are surfaced as errors
// with the response body attached.
func (a *Adapter) Send(ctx context.Context, n notifications.Notification) (string, error) {
	text := messageText(n)
	if text == "" {
		return "", fmt.Errorf("%w: empty message text", notifications.ErrInvalidInput)
	}

	raw, err := json.Marshal(slackRequest{Text: text})
	if err != nil {
		return "", fmt.Errorf("notifications/slack: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.webhookURL, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("notifications/slack: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("notifications/slack: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("notifications/slack: status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	// Slack incoming webhooks respond with a plain "ok" and no message id.
	return "", nil
}

// messageText folds the notification's subject and body into a single
// Slack message. The body is data["body"] when present, otherwise the
// template name (matching the resend adapter's rendering fallback).
func messageText(n notifications.Notification) string {
	subject := ""
	if n.Subject != nil {
		subject = strings.TrimSpace(*n.Subject)
	}
	body, _ := n.Data["body"].(string)
	body = strings.TrimSpace(body)
	if body == "" {
		body = strings.TrimSpace(n.Template)
	}
	switch {
	case subject != "" && body != "":
		return subject + "\n" + body
	case subject != "":
		return subject
	default:
		return body
	}
}
