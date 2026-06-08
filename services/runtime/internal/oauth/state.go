// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// stateTTL is how long a signed authorize state is valid for. Anything
// longer is a CSRF risk; anything shorter breaks slow human OAuth flows
// (consent screen + 2FA + browser tab switching).
const stateTTL = 10 * time.Minute

// State is the payload signed into the OAuth `state` parameter. It
// round-trips through the provider's consent screen and gets verified on
// callback to defeat CSRF.
//
// The nonce is a per-request random suffix the caller doesn't see; it
// guarantees two authorize calls produce distinct states even when
// tenant/user/return_to/issued_at all match. (Replay protection.)
type State struct {
	TenantID string `json:"t"`
	UserID   string `json:"u"`
	Provider string `json:"p"`
	ReturnTo string `json:"r,omitempty"`
	IssuedAt int64  `json:"i"`
	Nonce    string `json:"n"`
}

// SignState produces a base64url-encoded "<payload>.<hmac>" token. The
// secret is the runtime's AF_STACK_AUTH_SECRET — already validated as
// non-empty at boot by main.go. Empty secret returns an error rather
// than silently producing an unsigned token.
func SignState(secret string, s State) (string, error) {
	if secret == "" {
		return "", errors.New("oauth: cannot sign state with empty secret")
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("oauth: marshal state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmacHex(secret, encoded)
	return encoded + "." + mac, nil
}

// VerifyState parses + HMAC-checks a state token. Returns ErrInvalidState
// for any failure mode (bad format, bad signature, expired). The error
// purposefully collapses to one sentinel so a CSRF attacker can't tell
// "wrong format" from "wrong signature" from "wrong age".
func VerifyState(secret, token string) (State, error) {
	if secret == "" {
		return State{}, ErrInvalidState
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return State{}, ErrInvalidState
	}
	expected := hmacHex(secret, parts[0])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return State{}, ErrInvalidState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return State{}, ErrInvalidState
	}
	var s State
	if err := json.Unmarshal(payload, &s); err != nil {
		return State{}, ErrInvalidState
	}
	if s.IssuedAt == 0 {
		return State{}, ErrInvalidState
	}
	issued := time.Unix(s.IssuedAt, 0)
	if time.Since(issued) > stateTTL {
		return State{}, ErrInvalidState
	}
	// Reject states that come from the future (clock skew protection).
	if issued.After(time.Now().Add(2 * time.Minute)) {
		return State{}, ErrInvalidState
	}
	return s, nil
}

// hmacHex returns hex-encoded HMAC-SHA256 of payload using secret.
func hmacHex(secret, payload string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(payload))
	sum := h.Sum(nil)
	const hexChars = "0123456789abcdef"
	out := make([]byte, len(sum)*2)
	for i, b := range sum {
		out[i*2] = hexChars[b>>4]
		out[i*2+1] = hexChars[b&0x0f]
	}
	return string(out)
}

// ValidateReturnTo checks that returnTo is on one of the allowed origins.
// Empty returnTo is allowed (caller defaults). Allowed entries are full
// origins ("https://app.example.com") — we extract scheme+host from
// returnTo and compare. A nil or empty allowed list means "any same-host
// http(s) URL goes" — we still reject obvious javascript: / data: URLs.
func ValidateReturnTo(returnTo string, allowed []string) error {
	if returnTo == "" {
		return nil
	}
	u, err := url.Parse(returnTo)
	if err != nil {
		return ErrInvalidReturnTo
	}
	// Reject anything that isn't a hierarchical http(s) URL. Defeats
	// javascript:, data:, file:, vbscript:, etc.
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidReturnTo
	}
	if u.Host == "" {
		return ErrInvalidReturnTo
	}
	if len(allowed) == 0 {
		return nil
	}
	candidate := strings.ToLower(u.Scheme + "://" + u.Host)
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimRight(a, "/"), candidate) {
			return nil
		}
	}
	return ErrInvalidReturnTo
}
