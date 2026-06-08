// SPDX-License-Identifier: Apache-2.0

// Package google implements the Google OAuth 2.0 adapter for
// OAuth-on-behalf-of-user. The default scopes cover userinfo (email) and
// Google Calendar; callers override on /authorize when they need Drive,
// Gmail, etc.
//
// Endpoints (Google OAuth 2.0):
//
//	authorize  : https://accounts.google.com/o/oauth2/v2/auth
//	token      : https://oauth2.googleapis.com/token
//	revoke     : https://oauth2.googleapis.com/revoke
//
// access_type=offline + prompt=consent are appended so Google returns a
// refresh token. include_granted_scopes=true is also set so an incremental
// auth doesn't drop scopes already granted by an earlier flow.
package google

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

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// DefaultScopes is exposed so the factory can show them even when the
// provider isn't configured (dashboard preview).
var DefaultScopes = []string{
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/calendar",
}

// Endpoint URLs are package-level vars (not consts) so tests can swap
// them with an httptest server URL via reassignment + Cleanup.
var (
	authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenURL     = "https://oauth2.googleapis.com/token"
	revokeURL    = "https://oauth2.googleapis.com/revoke"
)

// Adapter implements the oauth.Provider contract for Google.
type Adapter struct {
	clientID     string
	clientSecret string
	http         *http.Client
}

// New constructs a Google adapter. The caller (factory) is responsible
// for ensuring clientID and clientSecret are non-empty.
func New(clientID, clientSecret string, client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         client,
	}
}

func (a *Adapter) Name() string            { return "google" }
func (a *Adapter) DefaultScopes() []string { return DefaultScopes }

// AuthorizeURL builds the consent URL. access_type=offline +
// prompt=consent ensure Google returns a refresh token even on
// re-consent. include_granted_scopes preserves previously granted scopes.
func (a *Adapter) AuthorizeURL(state string, scopes []string, redirectURI string) string {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("access_type", "offline")
	q.Set("include_granted_scopes", "true")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

// Exchange trades an authorization code for an access + refresh token.
func (a *Adapter) Exchange(ctx context.Context, code, redirectURI string) (oauthtypes.TokenSet, error) {
	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)
	return a.tokenRequest(ctx, form)
}

// Refresh trades a refresh token for a new access token. Google's
// response typically lacks a refresh_token field (refresh tokens are
// long-lived); Manager retains the existing one in that case.
func (a *Adapter) Refresh(ctx context.Context, refreshToken string) (oauthtypes.TokenSet, error) {
	if refreshToken == "" {
		return oauthtypes.TokenSet{}, oauthtypes.ErrRefreshNotSupported
	}
	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	return a.tokenRequest(ctx, form)
}

// Revoke calls Google's revoke endpoint. Returns nil on a 200/400 (400
// means "already revoked or unknown token"). Network errors propagate.
func (a *Adapter) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	form := url.Values{}
	form.Set("token", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("google: build revoke: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("google: revoke transport: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusBadRequest {
		// Google returns 400 for "token already revoked" — that's a no-op
		// success from the caller's perspective.
		return nil
	}
	return fmt.Errorf("google: revoke status %d", resp.StatusCode)
}

// tokenResponse models the canonical Google token endpoint response.
// Fields we don't need (id_token, token_type) are ignored.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (a *Adapter) tokenRequest(ctx context.Context, form url.Values) (oauthtypes.TokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("google: build token req: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: google: %v", oauthtypes.ErrExchangeFailed, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: google: decode %d: %v",
			oauthtypes.ErrExchangeFailed, resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		// NEVER log the raw body — it may contain the access token on
		// a partial-success path. Surface only the error code.
		desc := tr.Error
		if desc == "" {
			desc = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: google: %s", oauthtypes.ErrExchangeFailed, desc)
	}
	if tr.AccessToken == "" {
		return oauthtypes.TokenSet{}, errors.New("google: empty access token")
	}

	ts := oauthtypes.TokenSet{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
	}
	if tr.Scope != "" {
		ts.Scopes = strings.Split(tr.Scope, " ")
	}
	if tr.ExpiresIn > 0 {
		ts.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return ts, nil
}
