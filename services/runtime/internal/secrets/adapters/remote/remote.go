// SPDX-License-Identifier: Apache-2.0

// Package remote implements secrets.Store by talking the HTTP protocol
// from docs/adapters/protocols/secrets-v1.md to a sidecar (HashiCorp
// Vault, Doppler, AWS Secrets Manager, etc.). Selected by setting
// AF_STACK_SECRETS_ADAPTER=remote in the runtime.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// Adapter is a secrets.Store backed by a remote sidecar.
type Adapter struct {
	client *remote.Client
	name   string

	// Caps is exposed for the registry / dashboard.
	Caps Capabilities
}

// Capabilities is the typed view of the secrets capability payload.
type Capabilities struct {
	SupportsVersioning       bool   `json:"supports_versioning"`
	SupportsRotation         bool   `json:"supports_rotation"`
	SupportsRotationGenerate bool   `json:"supports_rotation_generate"`
	SupportsReveal           bool   `json:"supports_reveal"`
	SupportsMetadata         bool   `json:"supports_metadata"`
	KMSBackend               string `json:"kms_backend"`
	MaxValueBytes            int    `json:"max_value_bytes"`
	VersionRetentionCount    int    `json:"version_retention_count"`
	AuditLogRevealed         bool   `json:"audit_log_revealed"`
}

// Compile-time check.
var _ secrets.Store = (*Adapter)(nil)

// Config is the env-driven configuration.
type Config struct {
	BaseURL    string
	Token      string
	Timeout    time.Duration
	MaxRetries int
}

// New constructs an Adapter and verifies the sidecar speaks the
// protocol via a sync GET /v1/capabilities. Fails fast at boot time.
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("secrets/remote: BaseURL required")
	}
	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("secrets/remote: client: %w", err)
	}
	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("secrets/remote: capability probe: %w", err)
	}
	return a, nil
}

func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "secrets" {
		return fmt.Errorf("secrets/remote: adapter reports slot=%q; expected secrets", resp.Slot)
	}
	a.name = resp.Name
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &a.Caps); err != nil {
			return fmt.Errorf("secrets/remote: decode capabilities: %w", err)
		}
	}
	return nil
}

// Name returns the active adapter's name.
func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

// Probe satisfies registry.Probe by calling /healthz.
func (a *Adapter) Probe(ctx context.Context) (string, error) {
	h, err := a.client.Health(ctx)
	if err != nil {
		return "unhealthy", err
	}
	return h.Status, nil
}

// RawCapabilities returns the un-typed JSON from /v1/capabilities for
// the registry to forward unchanged.
func (a *Adapter) RawCapabilities(ctx context.Context) (json.RawMessage, error) {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	return resp.Capabilities, nil
}

// --- secrets.Store ------------------------------------------------------

// Get implements secrets.Store.Get by POSTing /v1/secrets/{key}/reveal.
func (a *Adapter) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPost,
		Path:     "/v1/secrets/" + url.PathEscape(key) + "/reveal",
		TenantID: tenantID,
		Body:     map[string]any{}, // empty body per protocol
	})
	if err != nil {
		if remote.IsCode(err, "secret_not_found") || remote.IsCode(err, "not_found") {
			return nil, secrets.ErrSecretNotFound
		}
		return nil, err
	}
	var out struct {
		Value string `json:"value"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("secrets/remote: decode reveal: %w", err)
	}
	return []byte(out.Value), nil
}

// GetMetadata implements secrets.Store via GET /v1/secrets/{key}.
func (a *Adapter) GetMetadata(ctx context.Context, tenantID, key string) (secrets.SecretMetadata, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodGet,
		Path:     "/v1/secrets/" + url.PathEscape(key),
		TenantID: tenantID,
	})
	if err != nil {
		if remote.IsCode(err, "secret_not_found") || remote.IsCode(err, "not_found") {
			return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
		}
		return secrets.SecretMetadata{}, err
	}
	var w metaWire
	if err := resp.DecodeJSON(&w); err != nil {
		return secrets.SecretMetadata{}, fmt.Errorf("secrets/remote: decode metadata: %w", err)
	}
	return wireToMeta(tenantID, w), nil
}

// Put implements secrets.Store via PUT /v1/secrets/{key}.
func (a *Adapter) Put(ctx context.Context, tenantID, key string, in secrets.PutInput) (secrets.SecretMetadata, error) {
	body := map[string]any{"value": in.Value}
	if strings.TrimSpace(in.Description) != "" {
		body["metadata"] = map[string]string{"description": in.Description}
	}
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPut,
		Path:     "/v1/secrets/" + url.PathEscape(key),
		TenantID: tenantID,
		Body:     body,
		IdempotencyKey: tenantID + "/" + key,
	})
	if err != nil {
		return secrets.SecretMetadata{}, err
	}
	var w metaWire
	if err := resp.DecodeJSON(&w); err != nil {
		return secrets.SecretMetadata{}, fmt.Errorf("secrets/remote: decode put: %w", err)
	}
	return wireToMeta(tenantID, w), nil
}

// Delete implements secrets.Store via DELETE /v1/secrets/{key}.
func (a *Adapter) Delete(ctx context.Context, tenantID, key string) error {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodDelete,
		Path:     "/v1/secrets/" + url.PathEscape(key),
		TenantID: tenantID,
	})
	if err != nil {
		if remote.IsCode(err, "secret_not_found") || remote.IsCode(err, "not_found") {
			return secrets.ErrSecretNotFound
		}
		return err
	}
	_ = resp.DecodeJSON(&struct{}{})
	return nil
}

// List implements secrets.Store via GET /v1/secrets.
func (a *Adapter) List(ctx context.Context, tenantID string) ([]secrets.SecretMetadata, error) {
	q := make(url.Values)
	q.Set("limit", "500") // generous; remote should paginate if more.
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodGet,
		Path:     "/v1/secrets",
		Query:    q,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Secrets []metaWire `json:"secrets"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("secrets/remote: decode list: %w", err)
	}
	res := make([]secrets.SecretMetadata, 0, len(out.Secrets))
	for _, w := range out.Secrets {
		res = append(res, wireToMeta(tenantID, w))
	}
	return res, nil
}

// Rotate implements secrets.Store via POST /v1/secrets/{key}/rotate
// (Mode B — caller-supplied value).
func (a *Adapter) Rotate(ctx context.Context, tenantID, key, newValue string) (secrets.SecretMetadata, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method:   http.MethodPost,
		Path:     "/v1/secrets/" + url.PathEscape(key) + "/rotate",
		TenantID: tenantID,
		Body:     map[string]any{"value": newValue},
	})
	if err != nil {
		if remote.IsCode(err, "secret_not_found") || remote.IsCode(err, "not_found") {
			return secrets.SecretMetadata{}, secrets.ErrSecretNotFound
		}
		return secrets.SecretMetadata{}, err
	}
	// /rotate response carries less info; fall back to GET metadata.
	var probe metaWire
	_ = resp.DecodeJSON(&probe)
	if probe.Key == "" {
		return a.GetMetadata(ctx, tenantID, key)
	}
	return wireToMeta(tenantID, probe), nil
}

// --- wire shapes ---------------------------------------------------------

type metaWire struct {
	Key           string            `json:"key"`
	Version       int               `json:"version"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
	LastRotatedAt string            `json:"last_rotated_at"`
	Metadata      map[string]string `json:"metadata"`
}

func wireToMeta(tenantID string, w metaWire) secrets.SecretMetadata {
	var tenantPtr *string
	if tenantID != "" {
		t := tenantID
		tenantPtr = &t
	}
	var descPtr *string
	if d, ok := w.Metadata["description"]; ok && d != "" {
		descPtr = &d
	}
	var rotateAfterPtr *string
	if w.LastRotatedAt != "" {
		s := w.LastRotatedAt
		rotateAfterPtr = &s
	}
	return secrets.SecretMetadata{
		Key:         w.Key,
		TenantID:    tenantPtr,
		Description: descPtr,
		RotateAfter: rotateAfterPtr,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
	}
}
