// SPDX-License-Identifier: Apache-2.0

// Package invitations implements token-based tenant membership invitations
// with an explicit accept / revoke / expire state machine.
//
// Split into two layers:
//
//   - This file: the pure model — Invitation struct, Status transitions,
//     and token generation/hashing. No I/O, fully unit-testable.
//   - store.go: Postgres persistence against suite_invitations (migration
//     00035_lifecycle.sql).
//
// Security model: the plaintext token is the accept capability. We store
// only its sha256 (token_hash) so a leak of the row can't be redeemed. The
// invitee has no tenant membership yet, so the accept lookup is by token
// (tenant-independent, under app.bypass_rls) rather than tenant-scoped.
package invitations

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Status is the lifecycle phase of an invitation. Mirrors the CHECK
// constraint on suite_invitations.status.
type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusRevoked  Status = "revoked"
	StatusExpired  Status = "expired"
)

// Sentinel errors. The REST layer maps each to a code + HTTP status.
var (
	// ErrNotFound: no invitation matched the token / id.
	ErrNotFound = errors.New("invitations: not found")
	// ErrNotPending: the invitation is not in the pending state (already
	// accepted or revoked) so the requested transition is illegal.
	ErrNotPending = errors.New("invitations: not pending")
	// ErrExpired: the invitation is pending but past its expires_at.
	ErrExpired = errors.New("invitations: expired")
	// ErrEmailMismatch: the accepting principal's email does not match the
	// invited email.
	ErrEmailMismatch = errors.New("invitations: email does not match invitation")
	// ErrInvalidInput: bad caller input (empty email, bad role, etc.).
	ErrInvalidInput = errors.New("invitations: invalid input")
)

// Invitation mirrors a suite_invitations row. Nullable columns use pointer
// types so JSON emits null. Token is never persisted; only TokenHash is.
type Invitation struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	Status     Status     `json:"status"`
	InvitedBy  *string    `json:"invited_by"`
	AcceptedBy *string    `json:"accepted_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// EffectiveStatus returns the invitation's status accounting for expiry: a
// stored 'pending' row whose expires_at has passed reports as 'expired'
// even before a cron flips the column. This is what callers should display
// and gate on.
func (inv Invitation) EffectiveStatus(now time.Time) Status {
	if inv.Status == StatusPending && !inv.ExpiresAt.IsZero() && !now.Before(inv.ExpiresAt) {
		return StatusExpired
	}
	return inv.Status
}

// CanAccept reports whether inv may transition pending → accepted at `now`.
// Returns nil when the accept is legal, or a sentinel describing why not.
// This is the single source of truth for the accept guard; the store calls
// it before writing so the DB and the in-memory checks never diverge.
func CanAccept(inv Invitation, now time.Time) error {
	switch inv.EffectiveStatus(now) {
	case StatusPending:
		return nil
	case StatusExpired:
		return ErrExpired
	case StatusAccepted, StatusRevoked:
		return ErrNotPending
	default:
		return ErrNotPending
	}
}

// CanRevoke reports whether inv may transition pending → revoked at `now`.
// An expired-but-still-pending invitation is revocable (it lets the inviter
// clean up), but an already-accepted or already-revoked one is not.
func CanRevoke(inv Invitation, _ time.Time) error {
	switch inv.Status {
	case StatusPending:
		return nil
	case StatusAccepted, StatusRevoked, StatusExpired:
		return ErrNotPending
	default:
		return ErrNotPending
	}
}

// EmailMatches reports whether the accepting principal's email matches the
// invited email (case-insensitive, trimmed). Enforced at accept time so a
// leaked token can't be redeemed by a different account.
func EmailMatches(invited, principal string) bool {
	return strings.EqualFold(strings.TrimSpace(invited), strings.TrimSpace(principal))
}

// DefaultTTL is the lifetime of a fresh invitation when the caller doesn't
// specify one.
const DefaultTTL = 7 * 24 * time.Hour

// GenerateToken returns a cryptographically-random, URL-safe invite token
// (the plaintext capability shown once to the inviter) alongside its
// sha256 hash (what the row stores). 30 bytes → 48 base32 chars.
func GenerateToken() (token, hash string, err error) {
	buf := make([]byte, 30)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = "inv_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	return token, HashToken(token), nil
}

// HashToken returns the hex sha256 of a token. Deterministic, so the accept
// path can hash the presented token and look the row up by token_hash.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
