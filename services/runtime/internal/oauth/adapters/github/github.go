// SPDX-License-Identifier: Apache-2.0

// Package github implements the GitHub user-token OAuth adapter.
//
// This is the OAuth-app flow, NOT the GitHub App flow. The user-token
// flow produces a non-expiring access token (no refresh) and runs as
// the user — what agents need for "act on behalf of me" use cases.
//
// Endpoints:
//
//	authorize : https://github.com/login/oauth/authorize
//	token     : https://github.com/login/oauth/access_token
//	revoke    : DELETE https://api.github.com/applications/{client_id}/grant
//
// Default scopes cover repos + basic user info. Callers narrow these on
// /authorize when finer grain is needed.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// DefaultScopes covers repos + basic user info — useful for code-aware
// agents and the demo "list my repos" flow.
var DefaultScopes = []string{"repo", "read:user"}

// Endpoint URLs are vars (not consts) so tests can swap them with an
// httptest server URL via package-level reassignment + Cleanup.
var (
	authorizeURL = "https://github.com/login/oauth/authorize"
	tokenURL     = "https://github.com/login/oauth/access_token"
	revokeBase   = "https://api.github.com/applications"
)

// Adapter implements oauth.Provider for GitHub user-token OAuth.
type Adapter struct {
	clientID     string
	clientSecret string
	http         *http.Client
}

func New(clientID, clientSecret string, client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{clientID: clientID, clientSecret: clientSecret, http: client}
}

func (a *Adapter) Name() string            { return "github" }
func (a *Adapter) DefaultScopes() []string { return DefaultScopes }

func (a *Adapter) AuthorizeURL(state string, scopes []string, redirectURI string) string {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

// Exchange returns the access token. GitHub returns a non-expiring
// token; ts.ExpiresAt stays zero, signalling Manager to skip refresh.
func (a *Adapter) Exchange(ctx context.Context, code, redirectURI string) (oauthtypes.TokenSet, error) {
	body, _ := json.Marshal(map[string]string{
		"client_id":     a.clientID,
		"client_secret": a.clientSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("github: build token req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: github: %v", oauthtypes.ErrExchangeFailed, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var tr struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: github: decode: %v", oauthtypes.ErrExchangeFailed, err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		errMsg := tr.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return oauthtypes.TokenSet{}, fmt.Errorf("%w: github: %s", oauthtypes.ErrExchangeFailed, errMsg)
	}
	ts := oauthtypes.TokenSet{
		AccessToken: tr.AccessToken,
		// ExpiresAt left zero — GitHub user tokens don't expire by
		// default; Manager treats zero-expiry as "no refresh".
	}
	if tr.Scope != "" {
		ts.Scopes = strings.Split(tr.Scope, ",")
		for i, s := range ts.Scopes {
			ts.Scopes[i] = strings.TrimSpace(s)
		}
	}
	return ts, nil
}

// Refresh always returns ErrRefreshNotSupported. GitHub user tokens do
// not expire by default; refresh tokens only exist on the GitHub App
// flow (which we don't ship here).
func (a *Adapter) Refresh(ctx context.Context, refreshToken string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, oauthtypes.ErrRefreshNotSupported
}

// Revoke uses GitHub's "delete grant" endpoint, which revokes ALL tokens
// the user has issued to the OAuth app (the user's authorization, not
// just the one token). Authenticated with Basic (client_id:client_secret).
func (a *Adapter) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"access_token": token})
	u := fmt.Sprintf("%s/%s/grant", revokeBase, url.PathEscape(a.clientID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("github: build revoke: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.SetBasicAuth(a.clientID, a.clientSecret)
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: revoke transport: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		// 204 = revoked, 404 = nothing to revoke. Both are success.
		return nil
	}
	return fmt.Errorf("github: revoke status %d", resp.StatusCode)
}
