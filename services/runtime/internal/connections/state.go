// SPDX-License-Identifier: Apache-2.0

package connections

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// stateTTL bounds how long a signed authorize state is valid. Long enough
// for a human consent screen + 2FA, short enough to blunt CSRF replay.
const stateTTL = 10 * time.Minute

// ErrInvalidState is returned when an OAuth callback state fails HMAC
// verification or has expired. It collapses every failure mode to one value
// so a CSRF attacker can't distinguish "bad format" from "bad signature"
// from "expired".
var ErrInvalidState = errors.New("connections: invalid oauth state")

// oauthState is the payload signed into the OAuth `state` parameter. Unlike
// the login-OAuth state (internal/oauth) this carries the requested scopes,
// connection name, and creator so the connection can be created at callback
// time with the right metadata — there is no pre-created row to reference.
type oauthState struct {
	Tenant    string   `json:"t"`
	Provider  string   `json:"p"`
	Name      string   `json:"n,omitempty"`
	Scopes    []string `json:"s,omitempty"`
	ReturnTo  string   `json:"r,omitempty"`
	CreatedBy string   `json:"c,omitempty"`
	IssuedAt  int64    `json:"i"`
	Nonce     string   `json:"x"`
}

// signState produces a base64url "<payload>.<hmac>" token bound to secret
// (the runtime's AF_STACK_AUTH_SECRET).
func signState(secret string, st oauthState) (string, error) {
	if secret == "" {
		return "", errors.New("connections: cannot sign state with empty secret")
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + stateHMAC(secret, encoded), nil
}

// verifyState parses + HMAC-checks a state token. Every failure returns
// ErrInvalidState.
func verifyState(secret, token string) (oauthState, error) {
	if secret == "" {
		return oauthState{}, ErrInvalidState
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return oauthState{}, ErrInvalidState
	}
	expected := stateHMAC(secret, parts[0])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return oauthState{}, ErrInvalidState
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return oauthState{}, ErrInvalidState
	}
	var st oauthState
	if err := json.Unmarshal(payload, &st); err != nil {
		return oauthState{}, ErrInvalidState
	}
	if st.IssuedAt == 0 {
		return oauthState{}, ErrInvalidState
	}
	issued := time.Unix(st.IssuedAt, 0)
	if time.Since(issued) > stateTTL {
		return oauthState{}, ErrInvalidState
	}
	if issued.After(time.Now().Add(2 * time.Minute)) {
		return oauthState{}, ErrInvalidState
	}
	return st, nil
}

func stateHMAC(secret, payload string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
