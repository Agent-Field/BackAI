// SPDX-License-Identifier: Apache-2.0

package invitations

import (
	"errors"
	"testing"
	"time"
)

func mkInv(status Status, expiresIn time.Duration, now time.Time) Invitation {
	return Invitation{
		ID:        "i1",
		TenantID:  "t1",
		Email:     "invitee@example.com",
		Role:      "member",
		Status:    status,
		CreatedAt: now,
		ExpiresAt: now.Add(expiresIn),
	}
}

// Contract: the accept guard is a state machine.
//
//	pending & unexpired  → accept allowed
//	pending & expired    → ErrExpired
//	accepted             → ErrNotPending
//	revoked              → ErrNotPending
func TestCanAccept_StateMachine(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		inv  Invitation
		want error
	}{
		{"pending unexpired accepts", mkInv(StatusPending, time.Hour, now), nil},
		{"pending expired rejected", mkInv(StatusPending, -time.Hour, now), ErrExpired},
		{"already accepted rejected", mkInv(StatusAccepted, time.Hour, now), ErrNotPending},
		{"revoked rejected", mkInv(StatusRevoked, time.Hour, now), ErrNotPending},
		{"expired-status rejected as expired", mkInv(StatusExpired, time.Hour, now), ErrExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanAccept(c.inv, now); !errors.Is(got, c.want) {
				t.Errorf("CanAccept = %v, want %v", got, c.want)
			}
		})
	}
}

// Contract: expiry is evaluated at the boundary — exactly at expires_at the
// invitation is expired (half-open [created, expires) window).
func TestEffectiveStatus_ExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	inv := mkInv(StatusPending, 0, now) // expires_at == now
	if got := inv.EffectiveStatus(now); got != StatusExpired {
		t.Errorf("at expires_at: EffectiveStatus = %q, want expired", got)
	}
	inv2 := mkInv(StatusPending, time.Nanosecond, now)
	if got := inv2.EffectiveStatus(now); got != StatusPending {
		t.Errorf("one ns before expiry: EffectiveStatus = %q, want pending", got)
	}
	// A non-pending row never spontaneously becomes expired.
	acc := mkInv(StatusAccepted, -time.Hour, now)
	if got := acc.EffectiveStatus(now); got != StatusAccepted {
		t.Errorf("accepted past-expiry: EffectiveStatus = %q, want accepted", got)
	}
}

// Contract: only pending invitations may be revoked.
func TestCanRevoke_StateMachine(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	if err := CanRevoke(mkInv(StatusPending, time.Hour, now), now); err != nil {
		t.Errorf("pending should be revocable, got %v", err)
	}
	// Even an expired-but-pending row is revocable (cleanup).
	if err := CanRevoke(mkInv(StatusPending, -time.Hour, now), now); err != nil {
		t.Errorf("expired-pending should be revocable, got %v", err)
	}
	if err := CanRevoke(mkInv(StatusAccepted, time.Hour, now), now); !errors.Is(err, ErrNotPending) {
		t.Errorf("accepted revoke = %v, want ErrNotPending", err)
	}
	if err := CanRevoke(mkInv(StatusRevoked, time.Hour, now), now); !errors.Is(err, ErrNotPending) {
		t.Errorf("revoked revoke = %v, want ErrNotPending", err)
	}
}

// Contract: accept requires the principal's email to match the invitation
// (case-insensitive, trimmed) — a leaked token can't be redeemed by anyone.
func TestEmailMatches(t *testing.T) {
	cases := []struct {
		invited, principal string
		want               bool
	}{
		{"a@b.com", "a@b.com", true},
		{"A@B.com", "a@b.com", true},
		{" a@b.com ", "a@b.com", true},
		{"a@b.com", "c@d.com", false},
		{"a@b.com", "", false},
	}
	for _, c := range cases {
		if got := EmailMatches(c.invited, c.principal); got != c.want {
			t.Errorf("EmailMatches(%q,%q) = %v, want %v", c.invited, c.principal, got, c.want)
		}
	}
}

// Contract: a generated token is one-time-shown plaintext whose sha256 is
// what the row stores; hashing is deterministic and re-derivable at accept.
func TestGenerateToken_HashRoundTrip(t *testing.T) {
	tok, hash, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken err: %v", err)
	}
	if tok == "" || hash == "" {
		t.Fatal("empty token/hash")
	}
	if HashToken(tok) != hash {
		t.Error("HashToken(token) must equal the returned hash")
	}
	// Distinct tokens on each call.
	tok2, _, _ := GenerateToken()
	if tok == tok2 {
		t.Error("two GenerateToken calls returned identical tokens")
	}
	// Hash never equals the plaintext (we never store plaintext).
	if hash == tok {
		t.Error("hash must differ from plaintext token")
	}
}
