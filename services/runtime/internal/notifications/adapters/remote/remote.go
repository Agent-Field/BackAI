// SPDX-License-Identifier: Apache-2.0

// Package remote implements notifications.Adapter by talking the HTTP
// protocol defined in docs/adapters/protocols/notifications-v1.md to a
// sidecar adapter. Selected via AF_STACK_NOTIFICATIONS_ADAPTER=remote.
//
// One Adapter per AF_STACK_NOTIFICATIONS_ADAPTER_URL. Goroutine-safe; the
// underlying remote.Client owns the connection pool.
package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

// Adapter is a notifications.Adapter backed by a remote sidecar speaking the
// notifications-v1 protocol.
type Adapter struct {
	client *remote.Client
	name   string // active adapter name from /v1/capabilities

	// CachedAt is the time we last refreshed capabilities. Exposed
	// for tests and the operator-facing status endpoints.
	CachedAt time.Time
}

// Compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

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
		return nil, errors.New("notifications/remote: BaseURL required")
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
		return nil, fmt.Errorf("notifications/remote: client: %w", err)
	}

	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("notifications/remote: capabilities probe: %w", err)
	}
	return a, nil
}

// refreshCapabilities pulls /v1/capabilities and extracts the adapter name.
func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "notifications" {
		return fmt.Errorf("notifications/remote: adapter reports slot=%q; expected notifications", resp.Slot)
	}
	a.name = resp.Name
	a.CachedAt = time.Now()
	return nil
}

// Name returns the active adapter's name (from /v1/capabilities). Used
// by the worker to populate the adapter column.
func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// Send implements notifications.Adapter by posting the notification to
// /v1/messages and extracting the provider_message_id from the response.
func (a *Adapter) Send(ctx context.Context, n notifications.Notification) (string, error) {
	body := notificationToWire(n)
	resp, err := a.client.Do(ctx, remote.Request{
		Method:         http.MethodPost,
		Path:           "/v1/messages",
		Body:           body,
		TenantID:       derefString(n.TenantID),
		IdempotencyKey: n.ID, // notification id is the natural idempotency key
	})
	if err != nil {
		// Check for specific error codes that need graceful handling.
		if remote.IsCode(err, "unsupported_channel") {
			return "", fmt.Errorf("%w: channel not supported", notifications.ErrAdapter)
		}
		if remote.IsCode(err, "template_not_found") {
			return "", fmt.Errorf("%w: template not found", notifications.ErrAdapter)
		}
		// 404 on retrieve/retry — treat as "not configured" for this kind/channel.
		if remote.IsCode(err, "not_found") {
			return "", fmt.Errorf("%w: adapter not found", notifications.ErrAdapter)
		}
		return "", err
	}

	var wire messageResponseWire
	if err := resp.DecodeJSON(&wire); err != nil {
		return "", fmt.Errorf("notifications/remote: decode response: %w", err)
	}

	return wire.ProviderMessageID, nil
}

// --- wire shapes ---------------------------------------------------------

// notificationToWire converts a notifications.Notification to the wire
// shape expected by POST /v1/messages per the protocol.
type messageRequestWire struct {
	Channel      string            `json:"channel"`
	To           []string          `json:"to"`
	From         string            `json:"from,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	BodyText     string            `json:"body_text,omitempty"`
	BodyHTML     string            `json:"body_html,omitempty"`
	ReplyTo      string            `json:"reply_to,omitempty"`
	TemplateID   string            `json:"template_id,omitempty"`
	TemplateVars map[string]any    `json:"template_vars,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

func notificationToWire(n notifications.Notification) messageRequestWire {
	return messageRequestWire{
		Channel:    string(n.Kind),
		To:         []string{n.To},
		From:       derefString(n.From),
		Subject:    derefString(n.Subject),
		TemplateID: n.Template,
		TemplateVars: n.Data,
		Metadata: map[string]any{
			"notification_id": n.ID,
		},
	}
}

// messageResponseWire mirrors the POST /v1/messages response shape.
type messageResponseWire struct {
	ID                string `json:"id"`
	ProviderMessageID string `json:"provider_message_id"`
	Status            string `json:"status"`
	SentAt            string `json:"sent_at,omitempty"`
}

// derefString safely dereferences a *string, returning "" if nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
