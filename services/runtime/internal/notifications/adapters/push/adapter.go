// SPDX-License-Identifier: Apache-2.0

// Package push implements the notifications.Adapter contract against
// Firebase Cloud Messaging (FCM) HTTP v1
// (https://firebase.google.com/docs/cloud-messaging/send-message).
//
// Each Send() POSTs a JSON message to
// https://fcm.googleapis.com/v1/projects/{projectID}/messages:send with
// a Bearer OAuth2 access token. The device registration token is taken
// from Notification.To; Subject maps to the FCM notification title and
// data["body"] (or the template name) to the notification body.
//
// The OAuth2 access token is injected via Config.AccessToken rather than
// minted from a service-account JSON inside the adapter: token minting
// (and its ~1h refresh cycle) belongs to the config/boot layer, keeping
// this adapter thin and testable.
//
// Configuration
//
//	ProjectID    required; the Firebase/GCP project id (path segment).
//	AccessToken  required; a short-lived OAuth2 bearer token with the
//	             https://www.googleapis.com/auth/firebase.messaging scope.
package push

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

// defaultBaseURL is the FCM v1 production host.
const defaultBaseURL = "https://fcm.googleapis.com"

// httpTimeout caps the per-request HTTP timeout.
const httpTimeout = 30 * time.Second

// ErrCredentialsMissing is returned by New when the project id or access
// token is absent.
var ErrCredentialsMissing = errors.New("notifications/push: project id and access token are required")

// Config holds adapter settings.
type Config struct {
	// ProjectID is the Firebase/GCP project id used in the FCM v1 path.
	// Required.
	ProjectID string

	// AccessToken is a short-lived OAuth2 bearer token for the FCM v1
	// API. Minted by the boot layer, not this adapter. Required.
	AccessToken string

	// BaseURL overrides the API endpoint (mainly for tests). Defaults to
	// defaultBaseURL.
	BaseURL string

	// HTTPClient lets tests inject a mocked transport. nil -> a client
	// with httpTimeout.
	HTTPClient *http.Client
}

// Adapter implements notifications.Adapter via FCM HTTP v1.
type Adapter struct {
	projectID   string
	accessToken string
	baseURL     string
	client      *http.Client
}

// compile-time interface check.
var _ notifications.Adapter = (*Adapter)(nil)

// New constructs the adapter. Returns ErrCredentialsMissing when the
// project id or access token is empty so callers fail fast at startup.
func New(_ context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
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
		projectID:   strings.TrimSpace(cfg.ProjectID),
		accessToken: strings.TrimSpace(cfg.AccessToken),
		baseURL:     baseURL,
		client:      client,
	}, nil
}

// Name returns the stable identifier persisted in the row.
func (a *Adapter) Name() string { return "fcm" }

// Configured reports whether the adapter has a project id and access
// token.
func (a *Adapter) Configured() bool {
	return a != nil && a.projectID != "" && a.accessToken != ""
}

// fcmMessage mirrors the FCM v1 request body: {"message": {...}}.
type fcmMessage struct {
	Message fcmMessageBody `json:"message"`
}

type fcmMessageBody struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}

type fcmNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// fcmResponse mirrors the FCM v1 success response:
// {"name": "projects/{p}/messages/{id}"}.
type fcmResponse struct {
	Name string `json:"name"`
}

// fcmErrorResponse mirrors the FCM v1 error envelope.
type fcmErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Send dispatches a push notification through FCM v1. The device
// registration token is Notification.To.
func (a *Adapter) Send(ctx context.Context, n notifications.Notification) (string, error) {
	token := strings.TrimSpace(n.To)
	if token == "" {
		return "", fmt.Errorf("%w: to (device registration token) required", notifications.ErrInvalidInput)
	}

	title := ""
	if n.Subject != nil {
		title = strings.TrimSpace(*n.Subject)
	}
	body, _ := n.Data["body"].(string)
	body = strings.TrimSpace(body)
	if body == "" {
		body = strings.TrimSpace(n.Template)
	}

	msg := fcmMessage{Message: fcmMessageBody{Token: token}}
	if title != "" || body != "" {
		msg.Message.Notification = &fcmNotification{Title: title, Body: body}
	}
	if data := stringData(n.Data); len(data) > 0 {
		msg.Message.Data = data
	}

	raw, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("notifications/push: marshal: %w", err)
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", a.baseURL, a.projectID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("notifications/push: new request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("notifications/push: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp fcmErrorResponse
		_ = json.Unmarshal(respBody, &errResp)
		detail := errResp.Error.Message
		if detail == "" {
			detail = strings.TrimSpace(string(respBody))
		}
		return "", fmt.Errorf("notifications/push: status %d: %s", resp.StatusCode, detail)
	}

	var decoded fcmResponse
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &decoded)
	}
	if decoded.Name == "" {
		return "", fmt.Errorf("%w: fcm returned no message name", notifications.ErrAdapter)
	}
	return decoded.Name, nil
}

// stringData converts the notification data map into FCM's string-only
// data payload. The reserved "body" key (used as the notification body)
// is skipped; all other values are stringified with fmt.Sprintf.
func stringData(data map[string]any) map[string]string {
	if len(data) == 0 {
		return nil
	}
	out := make(map[string]string, len(data))
	for k, v := range data {
		if k == "body" {
			continue
		}
		out[k] = fmt.Sprintf("%v", v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
