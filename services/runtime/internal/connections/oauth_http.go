// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// tokenResult is the parsed outcome of an exchange/refresh call.
type tokenResult struct {
	cred   credential
	expiry *time.Time
	scopes []string
}

// tokenResponse is the generic OAuth token endpoint JSON. Providers vary in
// extras (Slack nests bot tokens, etc.); the common fields cover
// GitHub/Google and the fake provider used in tests. Slack's nested shape is
// a live-integration follow-up.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// exchangeCode swaps an authorization code for tokens.
func (s *Service) exchangeCode(ctx context.Context, desc Descriptor, creds ClientCreds, code, redirectURI string) (tokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)
	return s.tokenRequest(ctx, desc, form)
}

// refreshToken trades a refresh token for a fresh token set.
func (s *Service) refreshToken(ctx context.Context, desc Descriptor, creds ClientCreds, refreshToken string) (tokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", creds.ClientID)
	form.Set("client_secret", creds.ClientSecret)
	return s.tokenRequest(ctx, desc, form)
}

// tokenRequest POSTs a form to the provider's token endpoint and parses the
// response. The upstream body is NEVER included verbatim in a returned error
// — a provider error page could echo the code/token. Only the provider's
// structured `error` code (if any) is surfaced.
func (s *Service) tokenRequest(ctx context.Context, desc Descriptor, form url.Values) (tokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, desc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResult{}, fmt.Errorf("%w: build request", ErrRefreshFailed)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return tokenResult{}, fmt.Errorf("%w: transport", ErrRefreshFailed)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokenResult{}, fmt.Errorf("%w: read body", ErrRefreshFailed)
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResult{}, fmt.Errorf("%w: non-JSON provider response (status %d)", ErrRefreshFailed, resp.StatusCode)
	}
	if tr.Error != "" {
		return tokenResult{}, fmt.Errorf("%w: provider error %q", ErrRefreshFailed, tr.Error)
	}
	if resp.StatusCode >= 400 {
		return tokenResult{}, fmt.Errorf("%w: provider status %d", ErrRefreshFailed, resp.StatusCode)
	}
	if tr.AccessToken == "" {
		return tokenResult{}, fmt.Errorf("%w: provider returned no access token", ErrRefreshFailed)
	}

	res := tokenResult{
		cred: credential{
			AccessToken:  tr.AccessToken,
			RefreshToken: tr.RefreshToken,
		},
		scopes: parseScopes(tr.Scope),
	}
	if tr.ExpiresIn > 0 {
		exp := s.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
		res.expiry = &exp
	}
	return res, nil
}

// parseScopes splits a provider scope string tolerantly on whitespace or
// commas (providers vary — GitHub uses spaces, some use commas).
func parseScopes(scope string) []string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	fields := strings.FieldsFunc(scope, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
