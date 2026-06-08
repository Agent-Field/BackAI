// SPDX-License-Identifier: Apache-2.0

// Package slack is a stub OAuth adapter for Slack user-token flow.
//
// TODO: implement against Slack's OAuth v2 user-token flow.
//   - authorize  : https://slack.com/oauth/v2/authorize (user_scope=...)
//   - token      : https://slack.com/api/oauth.v2.access
//   - revoke     : POST https://slack.com/api/auth.revoke
//   - tokens may expire; if so, rotation token in response.
//   - docs       : https://api.slack.com/authentication/oauth-v2
package slack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// DefaultScopes covers a minimal user-token surface.
var DefaultScopes = []string{"chat:write", "channels:read", "users:read"}

const authorizeURL = "https://slack.com/oauth/v2/authorize"

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

func (a *Adapter) Name() string            { return "slack" }
func (a *Adapter) DefaultScopes() []string { return DefaultScopes }

func (a *Adapter) AuthorizeURL(state string, scopes []string, redirectURI string) string {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", redirectURI)
	// user_scope is Slack's term for "act as user" scopes — different
	// from "scope" which is for bot tokens.
	q.Set("user_scope", strings.Join(scopes, ","))
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

func (a *Adapter) Exchange(ctx context.Context, code, redirectURI string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, fmt.Errorf("%w: slack adapter stub — TODO implement", oauthtypes.ErrExchangeFailed)
}

func (a *Adapter) Refresh(ctx context.Context, refreshToken string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, oauthtypes.ErrRefreshNotSupported
}

func (a *Adapter) Revoke(ctx context.Context, token string) error {
	return nil
}
