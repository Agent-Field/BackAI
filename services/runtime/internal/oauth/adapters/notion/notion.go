// SPDX-License-Identifier: Apache-2.0

// Package notion is a stub OAuth adapter for Notion. The Exchange /
// Refresh / Revoke methods return ErrExchangeFailed with a clear
// "implementation pending" message; the factory still registers the
// adapter when OAUTH_NOTION_CLIENT_ID / OAUTH_NOTION_CLIENT_SECRET are
// set so the dashboard surfaces it as configured.
//
// TODO: implement against Notion's OAuth 2.0 flow.
//   - authorize  : https://api.notion.com/v1/oauth/authorize
//   - token      : https://api.notion.com/v1/oauth/token (Basic auth)
//   - revoke     : POST https://api.notion.com/v1/oauth/revoke
//   - tokens are LONG-LIVED (no refresh); Refresh should keep returning
//     ErrRefreshNotSupported.
//   - docs       : https://developers.notion.com/docs/authorization
package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth/oauthtypes"
)

// DefaultScopes is empty for Notion — the OAuth installation page
// surfaces scope selection to the user, not the client.
var DefaultScopes = []string{}

const authorizeURL = "https://api.notion.com/v1/oauth/authorize"

// Adapter is a stub; the real flow can be filled in later without
// touching the wider system because the Provider contract is stable.
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

func (a *Adapter) Name() string            { return "notion" }
func (a *Adapter) DefaultScopes() []string { return DefaultScopes }

// AuthorizeURL builds a best-effort consent URL so the dashboard can
// kick off the redirect even though Exchange is not yet implemented.
// The OAuth flow will fail at Exchange until the TODO is finished.
func (a *Adapter) AuthorizeURL(state string, scopes []string, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("owner", "user")
	q.Set("state", state)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	return authorizeURL + "?" + q.Encode()
}

func (a *Adapter) Exchange(ctx context.Context, code, redirectURI string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, fmt.Errorf("%w: notion adapter stub — TODO implement", oauthtypes.ErrExchangeFailed)
}

func (a *Adapter) Refresh(ctx context.Context, refreshToken string) (oauthtypes.TokenSet, error) {
	return oauthtypes.TokenSet{}, oauthtypes.ErrRefreshNotSupported
}

func (a *Adapter) Revoke(ctx context.Context, token string) error {
	// Best effort: nothing to do until the real implementation lands.
	// Local soft-delete still happens in Manager.Disconnect.
	return nil
}
