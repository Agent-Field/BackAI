// SPDX-License-Identifier: Apache-2.0

// Package linear is a stub OAuth adapter for Linear (GraphQL API).
//
// TODO: implement against Linear's OAuth 2.0 flow.
//   - authorize  : https://linear.app/oauth/authorize
//   - token      : https://api.linear.app/oauth/token
//   - revoke     : POST https://api.linear.app/oauth/revoke
//   - tokens are LONG-LIVED (no refresh in standard flow).
//   - docs       : https://developers.linear.app/docs/oauth/authentication
package linear

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// DefaultScopes covers read access for issues + write for the user's own
// issues — broad enough for agent integrations.
var DefaultScopes = []string{"read", "write"}

const authorizeURL = "https://linear.app/oauth/authorize"

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

func (a *Adapter) Name() string            { return "linear" }
func (a *Adapter) DefaultScopes() []string { return DefaultScopes }

func (a *Adapter) AuthorizeURL(state string, scopes []string, redirectURI string) string {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, ","))
	q.Set("state", state)
	return authorizeURL + "?" + q.Encode()
}

func (a *Adapter) Exchange(ctx context.Context, code, redirectURI string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, fmt.Errorf("%w: linear adapter stub — TODO implement", oauthtypes.ErrExchangeFailed)
}

func (a *Adapter) Refresh(ctx context.Context, refreshToken string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, oauthtypes.ErrRefreshNotSupported
}

func (a *Adapter) Revoke(ctx context.Context, token string) error {
	return nil
}
