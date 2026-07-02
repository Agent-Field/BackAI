// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// refreshSkew is the window before expires_at at which Manager.Token
// proactively refreshes. 60s matches the spec; matches Google's
// recommended skew + leaves slack for clock drift between the runtime
// and the provider.
const refreshSkew = 60 * time.Second

// Manager owns the token lifecycle: it persists references in
// suite_oauth_tokens, stores actual values in the secrets vault, and
// refreshes-on-expiry transparently when callers fetch tokens.
//
// All public methods take a context that carries the resolved tenant id;
// the row insert/update binds tenant_id explicitly (Manager does NOT
// rely on RLS for the tenant scope — RLS is a defense-in-depth layer,
// not the authority). Vault.Put / Get use the tenant id explicitly.
type Manager struct {
	pool      *pgxpool.Pool
	vault     *secrets.Vault
	log       *slog.Logger
	refreshCh chan refreshLogEvent
}

type refreshLogEvent struct {
	TenantID    string
	Provider    string
	UserID      string
	Status      string
	ErrorCode   string
	AttemptedAt time.Time
}

// NewManager constructs a Manager. Both pool and vault must be non-nil
// — without persistence + vault encryption there's no safe way to ship
// OAuth.
func NewManager(pool *pgxpool.Pool, vault *secrets.Vault, log *slog.Logger) (*Manager, error) {
	if pool == nil {
		return nil, errors.New("oauth: manager requires a database pool")
	}
	if vault == nil {
		return nil, errors.New("oauth: manager requires a secrets vault")
	}
	if log == nil {
		log = slog.Default()
	}
	m := &Manager{pool: pool, vault: vault, log: log, refreshCh: make(chan refreshLogEvent, 1024)}
	go m.runRefreshLog()
	return m, nil
}

// vaultKey returns the secrets-vault key for a token. Refresh tokens
// use the same prefix plus ":refresh" so a single ListByPrefix could
// later enumerate every OAuth-related entry per tenant.
func vaultKey(provider, userID string, refresh bool) string {
	base := "oauth_" + provider + "_" + userID
	if refresh {
		return base + "_refresh"
	}
	return base
}

// StoreTokens persists a fresh TokenSet for (tenantID, userID, provider).
// Token VALUES go into the secrets vault; only references and metadata
// land in suite_oauth_tokens. Upserts on the unique key so a re-connect
// overwrites the previous tokens.
func (m *Manager) StoreTokens(
	ctx context.Context,
	tenantID, userID, provider string,
	ts TokenSet,
) error {
	if tenantID == "" {
		return ErrTenantRequired
	}
	if userID == "" {
		return ErrUserRequired
	}
	if provider == "" {
		return ErrProviderUnknown
	}
	if ts.AccessToken == "" {
		return fmt.Errorf("oauth: cannot store empty access token")
	}

	// Bind tenant so the RLS force policy permits the upsert on
	// suite_oauth_tokens. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")

	accessRef := vaultKey(provider, userID, false)
	if _, err := m.vault.Put(ctx, tenantID, accessRef, secrets.PutInput{
		Value:       ts.AccessToken,
		Description: "OAuth access token for " + provider,
	}); err != nil {
		return fmt.Errorf("oauth: store access token: %w", err)
	}

	var refreshRef *string
	if ts.RefreshToken != "" {
		r := vaultKey(provider, userID, true)
		if _, err := m.vault.Put(ctx, tenantID, r, secrets.PutInput{
			Value:       ts.RefreshToken,
			Description: "OAuth refresh token for " + provider,
		}); err != nil {
			return fmt.Errorf("oauth: store refresh token: %w", err)
		}
		refreshRef = &r
	}

	var expiresPtr *time.Time
	if !ts.ExpiresAt.IsZero() {
		t := ts.ExpiresAt
		expiresPtr = &t
	}

	// scopes default to [] so the column is never NULL — easier for the
	// connected-list query.
	scopes := ts.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	_, err := m.pool.Exec(ctx, `
		insert into suite_oauth_tokens
		    (tenant_id, user_id, provider, access_token_ref, refresh_token_ref,
		     scopes, expires_at, created_at, updated_at, revoked_at)
		values ($1, $2, $3, $4, $5, $6, $7, now(), now(), null)
		on conflict (tenant_id, user_id, provider) do update set
		    access_token_ref  = excluded.access_token_ref,
		    refresh_token_ref = excluded.refresh_token_ref,
		    scopes            = excluded.scopes,
		    expires_at        = excluded.expires_at,
		    updated_at        = now(),
		    revoked_at        = null
	`, tenantID, userID, provider, accessRef, refreshRef, scopes, expiresPtr)
	if err != nil {
		return fmt.Errorf("oauth: upsert token row: %w", err)
	}
	m.log.Info("oauth: tokens stored",
		"tenant_id", tenantID,
		"user_id", userID,
		"provider", provider,
		"scopes", scopes,
		"has_refresh", refreshRef != nil,
		"has_expiry", expiresPtr != nil,
	)
	return nil
}

// Token returns a fresh access token for (tenantID, userID, provider).
// If the stored token is within refreshSkew of expiry, refresh first.
// Returns ErrConnectionNotFound when no active row exists.
//
// p is the Provider implementation used to refresh; pass nil to skip
// the refresh path (returns the stored token even if near expiry —
// useful for tests).
func (m *Manager) Token(
	ctx context.Context,
	tenantID, userID, provider string,
	p Provider,
) (TokenSet, error) {
	if tenantID == "" {
		return TokenSet{}, ErrTenantRequired
	}
	if userID == "" {
		return TokenSet{}, ErrUserRequired
	}

	row, err := m.loadRow(ctx, tenantID, userID, provider)
	if err != nil {
		return TokenSet{}, err
	}

	access, err := m.vault.Get(ctx, tenantID, row.AccessTokenRef)
	if err != nil {
		return TokenSet{}, fmt.Errorf("oauth: load access token from vault: %w", err)
	}

	ts := TokenSet{
		AccessToken: string(access),
		Scopes:      row.Scopes,
	}
	if row.ExpiresAt != nil {
		ts.ExpiresAt = *row.ExpiresAt
	}

	// Refresh path: when expiry is set, within skew, and we have both
	// a refresh ref and a Provider that can refresh.
	if row.ExpiresAt != nil && time.Until(*row.ExpiresAt) < refreshSkew {
		if p == nil || row.RefreshTokenRef == nil {
			m.log.Warn("oauth: token expired and refresh not available",
				"tenant_id", tenantID,
				"user_id", userID,
				"provider", provider,
				"has_provider", p != nil,
				"has_refresh_ref", row.RefreshTokenRef != nil,
			)
			// Return the stale token — caller will get a 401 from the
			// provider and can re-initiate the OAuth flow.
			return ts, nil
		}
		refresh, err := m.vault.Get(ctx, tenantID, *row.RefreshTokenRef)
		if err != nil {
			m.recordRefreshAttempt(ctx, refreshLogEvent{
				TenantID:  tenantID,
				UserID:    userID,
				Provider:  provider,
				Status:    "failed",
				ErrorCode: "refresh_token_load_failed",
			})
			return TokenSet{}, fmt.Errorf("oauth: load refresh token from vault: %w", err)
		}
		next, err := p.Refresh(ctx, string(refresh))
		if err != nil {
			m.recordRefreshAttempt(ctx, refreshLogEvent{
				TenantID:  tenantID,
				UserID:    userID,
				Provider:  provider,
				Status:    "failed",
				ErrorCode: refreshErrorCode(err),
			})
			return TokenSet{}, fmt.Errorf("oauth: refresh failed: %w", err)
		}
		// Providers vary: Google returns a new refresh token only on
		// rotation; if next.RefreshToken is empty, retain the old one.
		if next.RefreshToken == "" {
			next.RefreshToken = string(refresh)
		}
		// Scopes may carry through unchanged from the provider; if the
		// next set is empty, keep the previously authorised scopes.
		if len(next.Scopes) == 0 {
			next.Scopes = row.Scopes
		}
		if err := m.StoreTokens(ctx, tenantID, userID, provider, next); err != nil {
			m.recordRefreshAttempt(ctx, refreshLogEvent{
				TenantID:  tenantID,
				UserID:    userID,
				Provider:  provider,
				Status:    "failed",
				ErrorCode: "refresh_store_failed",
			})
			return TokenSet{}, err
		}
		m.recordRefreshAttempt(ctx, refreshLogEvent{
			TenantID: tenantID,
			UserID:   userID,
			Provider: provider,
			Status:   "success",
		})
		return next, nil
	}

	return ts, nil
}

func (m *Manager) recordRefreshAttempt(_ context.Context, ev refreshLogEvent) {
	if m == nil || m.pool == nil || m.refreshCh == nil {
		return
	}
	if ev.AttemptedAt.IsZero() {
		ev.AttemptedAt = time.Now().UTC()
	}
	select {
	case m.refreshCh <- ev:
	default:
		m.log.Warn("oauth: dropping refresh history event; buffer full",
			"tenant_id", ev.TenantID,
			"provider", ev.Provider,
			"status", ev.Status,
		)
	}
}

func (m *Manager) runRefreshLog() {
	for ev := range m.refreshCh {
		m.insertRefreshLog(ev)
	}
}

func (m *Manager) insertRefreshLog(ev refreshLogEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		m.log.Warn("oauth: refresh history begin failed", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "set local app.bypass_rls = 'on'"); err != nil {
		m.log.Warn("oauth: refresh history bypass failed", "error", err)
		return
	}
	var errorCode *string
	if ev.ErrorCode != "" {
		errorCode = &ev.ErrorCode
	}
	_, err = tx.Exec(ctx, `
        insert into suite_oauth_refresh_log
          (tenant_id, provider, user_id, status, error_code, attempted_at)
        values ($1, $2, $3, $4, $5, $6)
    `, ev.TenantID, ev.Provider, ev.UserID, ev.Status, errorCode, ev.AttemptedAt)
	if err != nil {
		m.log.Warn("oauth: refresh history insert failed",
			"tenant_id", ev.TenantID,
			"provider", ev.Provider,
			"status", ev.Status,
			"error", err,
		)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		m.log.Warn("oauth: refresh history commit failed", "error", err)
	}
}

func refreshErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if code == "" {
		return "refresh_failed"
	}
	if len(code) > 160 {
		code = code[:160]
	}
	return code
}

// List returns the active (non-revoked) connections for (tenantID,
// userID). Refresh tokens and access tokens NEVER leave the vault here
// — only metadata.
func (m *Manager) List(ctx context.Context, tenantID, userID string) ([]Connection, error) {
	if tenantID == "" {
		return nil, ErrTenantRequired
	}
	if userID == "" {
		return nil, ErrUserRequired
	}
	rows, err := m.pool.Query(ctx, `
		select provider, scopes, expires_at, created_at
		from suite_oauth_tokens
		where tenant_id = $1 and user_id = $2 and revoked_at is null
		order by provider asc
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("oauth: list connections: %w", err)
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var (
			provider  string
			scopes    []string
			expiresAt *time.Time
			createdAt time.Time
		)
		if err := rows.Scan(&provider, &scopes, &expiresAt, &createdAt); err != nil {
			return nil, fmt.Errorf("oauth: scan connection: %w", err)
		}
		c := Connection{
			Provider:    provider,
			Scopes:      scopes,
			ConnectedAt: createdAt,
		}
		if expiresAt != nil {
			t := *expiresAt
			c.ExpiresAt = &t
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("oauth: list rows: %w", err)
	}
	return out, nil
}

// Disconnect best-effort revokes the provider's token, deletes the vault
// entries, and soft-deletes the suite_oauth_tokens row. Returns
// ErrConnectionNotFound when no active row exists. Revoke failures are
// logged but don't fail Disconnect — the row is gone locally, which is
// what the user asked for.
func (m *Manager) Disconnect(
	ctx context.Context,
	tenantID, userID, provider string,
	p Provider,
) error {
	if tenantID == "" {
		return ErrTenantRequired
	}
	if userID == "" {
		return ErrUserRequired
	}
	// Bind tenant so the RLS force policy permits the load + soft-delete
	// update on suite_oauth_tokens. "" apiKeyID is safe.
	ctx = tenantctx.WithTenant(ctx, tenantID, "")
	row, err := m.loadRow(ctx, tenantID, userID, provider)
	if err != nil {
		return err
	}

	// Best-effort upstream revoke. Errors are logged, not returned.
	if p != nil {
		access, getErr := m.vault.Get(ctx, tenantID, row.AccessTokenRef)
		if getErr == nil {
			if revErr := p.Revoke(ctx, string(access)); revErr != nil {
				m.log.Warn("oauth: upstream revoke failed",
					"tenant_id", tenantID,
					"user_id", userID,
					"provider", provider,
					"error", revErr,
				)
			}
		}
	}

	// Delete vault entries. Missing entries are fine (vault may have
	// been cleared independently).
	if err := m.vault.Delete(ctx, tenantID, row.AccessTokenRef); err != nil &&
		!errors.Is(err, secrets.ErrSecretNotFound) {
		m.log.Warn("oauth: delete access token from vault failed",
			"tenant_id", tenantID,
			"provider", provider,
			"error", err,
		)
	}
	if row.RefreshTokenRef != nil {
		if err := m.vault.Delete(ctx, tenantID, *row.RefreshTokenRef); err != nil &&
			!errors.Is(err, secrets.ErrSecretNotFound) {
			m.log.Warn("oauth: delete refresh token from vault failed",
				"tenant_id", tenantID,
				"provider", provider,
				"error", err,
			)
		}
	}

	// Soft-delete: keep the row for audit purposes (when/who connected,
	// when it was revoked) but make List ignore it.
	_, err = m.pool.Exec(ctx, `
		update suite_oauth_tokens
		set revoked_at = now(), updated_at = now()
		where tenant_id = $1 and user_id = $2 and provider = $3
		  and revoked_at is null
	`, tenantID, userID, provider)
	if err != nil {
		return fmt.Errorf("oauth: soft-delete row: %w", err)
	}
	m.log.Info("oauth: disconnected",
		"tenant_id", tenantID,
		"user_id", userID,
		"provider", provider,
	)
	return nil
}

// tokenRow is the internal projection of a suite_oauth_tokens row used
// by Manager. Token values are NOT carried here — only references.
type tokenRow struct {
	AccessTokenRef  string
	RefreshTokenRef *string
	Scopes          []string
	ExpiresAt       *time.Time
}

func (m *Manager) loadRow(ctx context.Context, tenantID, userID, provider string) (tokenRow, error) {
	var row tokenRow
	err := m.pool.QueryRow(ctx, `
		select access_token_ref, refresh_token_ref, scopes, expires_at
		from suite_oauth_tokens
		where tenant_id = $1 and user_id = $2 and provider = $3
		  and revoked_at is null
	`, tenantID, userID, provider).Scan(
		&row.AccessTokenRef, &row.RefreshTokenRef, &row.Scopes, &row.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenRow{}, ErrConnectionNotFound
		}
		return tokenRow{}, fmt.Errorf("oauth: load row: %w", err)
	}
	return row, nil
}

// RandomNonce returns a 16-byte hex string used as the state nonce.
// Cryptographically random; collisions are negligible. Exposed so the
// server package can populate State.Nonce.
func RandomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
