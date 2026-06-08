// SPDX-License-Identifier: Apache-2.0

// Package oauthtypes carries the data types shared between the oauth
// package and its adapters. Extracted to a leaf package so the
// per-provider adapters in oauth/adapters/* don't import oauth itself
// (which would cause an import cycle with oauth/factory.go).
package oauthtypes

import (
	"errors"
	"time"
)

// TokenSet is the canonical OAuth result. ExpiresAt is the zero value
// when the provider issues a token with no expiry (GitHub user tokens,
// Notion).
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	Scopes       []string
	ExpiresAt    time.Time
}

// ErrRefreshNotSupported is returned by adapters whose flow does not
// produce a refresh token (GitHub user tokens, Notion). The Manager
// catches this and surfaces a graceful "reconnect required" path.
//
// Defined here (not in oauth/interface.go) so adapters can return it
// without importing the oauth package.
var ErrRefreshNotSupported = errors.New("oauth: refresh not supported by provider")

// ErrExchangeFailed wraps a provider Exchange/Refresh failure with the
// upstream HTTP body stripped (provider error pages may include tokens).
var ErrExchangeFailed = errors.New("oauth: token exchange failed")
