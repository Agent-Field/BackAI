// SPDX-License-Identifier: Apache-2.0

// Package remote implements auth.Provider by talking the HTTP protocol
// from docs/adapters/protocols/auth-v1.md to a sidecar (better-auth,
// Auth0, Clerk, etc.). Selected by setting AF_STACK_AUTH_ADAPTER=remote
// in the runtime.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/auth"
)

// Adapter is an auth.Provider backed by a remote sidecar.
type Adapter struct {
	client *remote.Client
	name   string

	// Caps is exposed for the registry / dashboard.
	Caps auth.Capabilities
}

// Compile-time check.
var _ auth.Provider = (*Adapter)(nil)

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
		return nil, errors.New("auth/remote: BaseURL required")
	}
	c, err := remote.NewClient(remote.Config{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("auth/remote: client: %w", err)
	}
	a := &Adapter{client: c}
	if err := a.refreshCapabilities(ctx); err != nil {
		return nil, fmt.Errorf("auth/remote: capability probe: %w", err)
	}
	return a, nil
}

func (a *Adapter) refreshCapabilities(ctx context.Context) error {
	resp, err := a.client.Capabilities(ctx)
	if err != nil {
		return err
	}
	if resp.Slot != "auth" {
		return fmt.Errorf("auth/remote: adapter reports slot=%q; expected auth", resp.Slot)
	}
	a.name = resp.Name
	if len(resp.Capabilities) > 0 {
		if err := json.Unmarshal(resp.Capabilities, &a.Caps); err != nil {
			return fmt.Errorf("auth/remote: decode capabilities: %w", err)
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

// --- auth.Provider ------------------------------------------------------

// VerifySession implements auth.Provider via POST /v1/sessions/verify.
func (a *Adapter) VerifySession(ctx context.Context, token string) (auth.Identity, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/sessions/verify",
		Body:   map[string]any{"token": token},
	})
	if err != nil {
		if remote.IsCode(err, "invalid_token") {
			return auth.Identity{}, auth.ErrInvalidToken
		}
		if remote.IsCode(err, "expired_token") {
			return auth.Identity{}, auth.ErrExpiredToken
		}
		return auth.Identity{}, err
	}
	var out identityWire
	if err := resp.DecodeJSON(&out); err != nil {
		return auth.Identity{}, fmt.Errorf("auth/remote: decode verify: %w", err)
	}
	return wireToIdentity(out), nil
}

// RefreshSession implements auth.Provider via POST /v1/sessions/refresh.
// Adapters that don't issue refresh tokens MAY 404 — translated here
// to ErrRefreshNotSupported so callers can fall back gracefully.
func (a *Adapter) RefreshSession(ctx context.Context, refreshToken string) (auth.RefreshedSession, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/sessions/refresh",
		Body:   map[string]string{"refresh_token": refreshToken},
	})
	if err != nil {
		switch {
		case remote.IsCode(err, "not_found"):
			return auth.RefreshedSession{}, auth.ErrRefreshNotSupported
		case remote.IsCode(err, "invalid_token"):
			return auth.RefreshedSession{}, auth.ErrInvalidToken
		case remote.IsCode(err, "expired_token"):
			return auth.RefreshedSession{}, auth.ErrExpiredToken
		}
		return auth.RefreshedSession{}, err
	}
	var out refreshWire
	if err := resp.DecodeJSON(&out); err != nil {
		return auth.RefreshedSession{}, fmt.Errorf("auth/remote: decode refresh: %w", err)
	}
	rs := auth.RefreshedSession{
		Token:        out.Token,
		RefreshToken: out.RefreshToken,
		UserID:       out.UserID,
	}
	if out.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, out.ExpiresAt); err == nil {
			rs.ExpiresAt = t
		}
	}
	return rs, nil
}

// RevokeSession implements auth.Provider via POST /v1/sessions/revoke.
// Idempotent: 204 No Content on success or for already-invalid tokens.
func (a *Adapter) RevokeSession(ctx context.Context, token string) error {
	_, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodPost,
		Path:   "/v1/sessions/revoke",
		Body:   map[string]string{"token": token},
	})
	if err != nil {
		if remote.IsCode(err, "invalid_token") {
			return auth.ErrInvalidToken
		}
		return err
	}
	return nil
}

// GetUser implements auth.Provider via GET /v1/users/{id}.
func (a *Adapter) GetUser(ctx context.Context, id string) (auth.User, error) {
	resp, err := a.client.Do(ctx, remote.Request{
		Method: http.MethodGet,
		Path:   "/v1/users/" + id,
	})
	if err != nil {
		if remote.IsCode(err, "user_not_found") || remote.IsCode(err, "not_found") {
			return auth.User{}, auth.ErrUserNotFound
		}
		return auth.User{}, err
	}
	var out userWire
	if err := resp.DecodeJSON(&out); err != nil {
		return auth.User{}, fmt.Errorf("auth/remote: decode user: %w", err)
	}
	return wireToUser(out), nil
}

// Capabilities returns the cached capabilities from the adapter.
func (a *Adapter) Capabilities() auth.Capabilities {
	if a == nil {
		return auth.Capabilities{}
	}
	return a.Caps
}

// --- wire shapes ---------------------------------------------------------

type refreshWire struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	UserID       string `json:"user_id"`
}

type identityWire struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	ExpiresAt   string   `json:"expires_at"`
	MFAVerified bool     `json:"mfa_verified"`
}

type userWire struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	Name        string   `json:"name"`
	CreatedAt   string   `json:"created_at"`
	MFAEnrolled bool     `json:"mfa_enrolled"`
	Providers   []string `json:"providers"`
}

func wireToIdentity(w identityWire) auth.Identity {
	expiresAt, _ := time.Parse(time.RFC3339, w.ExpiresAt)
	roles := w.Roles
	if roles == nil {
		roles = []string{}
	}
	return auth.Identity{
		UserID:      w.UserID,
		Email:       w.Email,
		TenantID:    w.TenantID,
		Roles:       roles,
		ExpiresAt:   expiresAt,
		MFAVerified: w.MFAVerified,
	}
}

func wireToUser(w userWire) auth.User {
	createdAt, _ := time.Parse(time.RFC3339, w.CreatedAt)
	providers := w.Providers
	if providers == nil {
		providers = []string{}
	}
	return auth.User{
		ID:          w.ID,
		Email:       w.Email,
		Name:        w.Name,
		CreatedAt:   createdAt,
		MFAEnrolled: w.MFAEnrolled,
		Providers:   providers,
	}
}
