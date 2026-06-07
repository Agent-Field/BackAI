// Package resend implements the notifications.Adapter contract against
// Resend (https://resend.com), the email-as-API service.
//
// Each Send() POSTs to https://api.resend.com/emails with a Bearer
// token. The Notification.Template is rendered as a tiny mustache-style
// text substitution on Notification.Data — v1 keeps the renderer
// intentionally simple; production deployments that want full React
// Email or MJML should swap in a richer renderer at the Service layer.
//
// Configuration
//
//	RESEND_API_KEY  required; passed in the Authorization header.
//	AF_STACK_NOTIFICATIONS_FROM optional default "from" address used
//	when a notification doesn't supply one.
package resend

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

// DefaultBaseURL is Resend's production endpoint.
const DefaultBaseURL = "https://api.resend.com"

// httpTimeout caps the per-request HTTP timeout.
const httpTimeout = 30 * time.Second

// ErrAPIKeyMissing is returned by New when no API key is configured.
var ErrAPIKeyMissing = errors.New("notifications/resend: api key required (set RESEND_API_KEY)")

// Config holds adapter settings.
type Config struct {
	// APIKey is the Resend secret key. Required.
	APIKey string

	// DefaultFrom is the from-address fallback used when a notification
	// doesn't supply one. Resend requires a verified domain.
	DefaultFrom string

	// BaseURL overrides the API endpoint (mainly for tests).
	BaseURL string

	// HTTPClient lets tests inject a mocked transport. nil -> a client
	// with httpTimeout.
	HTTPClient *http.Client
}

// Adapter implements notifications.Adapter via Resend.
type Adapter struct {
	apiKey      string
	defaultFrom string
	baseURL     string
	client      *http.Client
}

// compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

// New constructs the adapter. Returns ErrAPIKeyMissing when APIKey is
// empty so callers fail fast at startup rather than at first Send.
func New(cfg Config) (*Adapter, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyMissing
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: httpTimeout}
	}
	return &Adapter{
		apiKey:      cfg.APIKey,
		defaultFrom: cfg.DefaultFrom,
		baseURL:     baseURL,
		client:      client,
	}, nil
}

// Name returns the stable identifier persisted in the row.
func (a *Adapter) Name() string { return "resend" }

// emailRequest mirrors Resend's POST /emails body shape. We send a
// text body by default — callers that want HTML supply
// data["html"]=true and a "body" field with the markup.
type emailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}

type emailResponse struct {
	ID string `json:"id"`
}

// Send dispatches the notification through Resend's HTTP API.
//
// Template rendering is intentionally simple: any "{{key}}" tokens in
// the body (when present in data) are replaced with the string value of
// data[key]. Missing keys are left as-is so unknown templates degrade
// to a visible placeholder rather than failing silently.
func (a *Adapter) Send(ctx context.Context, n notifications.Notification) (string, error) {
	if n.Kind != notifications.KindEmail {
		return "", fmt.Errorf("notifications/resend: kind %q not supported (email only)", n.Kind)
	}

	from := a.defaultFrom
	if n.From != nil && *n.From != "" {
		from = *n.From
	}
	if from == "" {
		return "", errors.New("notifications/resend: from address required (set AF_STACK_NOTIFICATIONS_FROM or notification.from)")
	}
	subject := ""
	if n.Subject != nil {
		subject = *n.Subject
	}
	if subject == "" {
		subject = "(no subject)"
	}

	body := renderTemplate(n.Template, n.Data)
	req := emailRequest{
		From:    from,
		To:      []string{n.To},
		Subject: render(subject, n.Data),
		Text:    body,
	}
	// If caller indicated HTML, swap the body field.
	if v, ok := n.Data["html"]; ok {
		if b, ok := v.(bool); ok && b {
			req.HTML = body
			req.Text = ""
		}
	}

	raw, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("notifications/resend: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.baseURL+"/emails", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("notifications/resend: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("notifications/resend: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("notifications/resend: status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded emailResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &decoded); err != nil {
			// Non-fatal: Resend returned 2xx but we couldn't parse.
			// Surface a synthetic id so the row still reflects success.
			return "resend_unknown", nil
		}
	}
	if decoded.ID == "" {
		return "resend_unknown", nil
	}
	return decoded.ID, nil
}

// renderTemplate is the v1 template engine.
//
// Phase 10.1 ships a plain "{{key}}" substitution against the
// notification's data map. The template string is the body itself for
// the "default" template; richer templates (file-loaded, MJML, React
// Email) land in Phase 10.2+.
func renderTemplate(name string, data map[string]any) string {
	body, _ := data["body"].(string)
	if body == "" {
		// Fall back to the template name itself — useful for tests and
		// for the "log" path where the body isn't important.
		body = name
	}
	return render(body, data)
}

// render replaces {{key}} tokens with str(data[key]).
func render(s string, data map[string]any) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	out := s
	for k, v := range data {
		token := "{{" + k + "}}"
		out = strings.ReplaceAll(out, token, fmt.Sprintf("%v", v))
	}
	return out
}
