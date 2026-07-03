// SPDX-License-Identifier: Apache-2.0

// settings.go — operator-panel billing configuration (suite_billing_settings).
//
// The dashboard's Platform → Billing page stores the Stripe secret key and
// webhook secret here, envelope-encrypted with the same KMS-backed cipher
// as the tenant secrets vault. On save the REST layer swaps the live
// billing client (Service.SwapClient), so keys take effect without a
// restart. Env vars (STRIPE_SECRET_KEY / STRIPE_WEBHOOK_SECRET) remain a
// valid override for infra-as-code deployments: vault-stored settings win
// when present, env fills the gaps.

package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsCipher is the envelope-encryption surface SettingsStore needs.
// *secrets.Cipher satisfies it.
type SettingsCipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(envelope []byte) ([]byte, error)
}

// Setting keys.
const (
	SettingStripeSecretKey     = "stripe_secret_key"
	SettingStripeWebhookSecret = "stripe_webhook_secret"
)

// SettingsStore reads/writes encrypted billing settings.
type SettingsStore struct {
	pool   *pgxpool.Pool
	cipher SettingsCipher
}

// NewSettingsStore returns a store; pool/cipher may be nil (all reads
// return empty, writes error) so boot stays graceful without a DB or KMS.
func NewSettingsStore(pool *pgxpool.Pool, cipher SettingsCipher) *SettingsStore {
	return &SettingsStore{pool: pool, cipher: cipher}
}

func (s *SettingsStore) ready() bool {
	return s != nil && s.pool != nil && s.cipher != nil
}

// Set writes (or clears, when value is empty) one setting.
func (s *SettingsStore) Set(ctx context.Context, key, value string) error {
	if !s.ready() {
		return fmt.Errorf("%w: settings store not configured (db + kms required)", ErrBillingUnavailable)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("%w: setting key is required", ErrInvalidInput)
	}
	if strings.TrimSpace(value) == "" {
		_, err := s.pool.Exec(ctx, `delete from suite_billing_settings where key = $1`, key)
		if err != nil {
			return fmt.Errorf("billing: clear setting %s: %w", key, err)
		}
		return nil
	}
	enc, err := s.cipher.Encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("billing: encrypt setting %s: %w", key, err)
	}
	if _, err := s.pool.Exec(ctx, `
		insert into suite_billing_settings (key, value_enc, updated_at)
		values ($1, $2, now())
		on conflict (key) do update set value_enc = excluded.value_enc, updated_at = now()`,
		key, enc); err != nil {
		return fmt.Errorf("billing: put setting %s: %w", key, err)
	}
	return nil
}

// Get returns the decrypted value, or "" when unset.
func (s *SettingsStore) Get(ctx context.Context, key string) (string, error) {
	if !s.ready() {
		return "", nil
	}
	var enc []byte
	err := s.pool.QueryRow(ctx,
		`select value_enc from suite_billing_settings where key = $1`, key).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("billing: get setting %s: %w", key, err)
	}
	plain, err := s.cipher.Decrypt(enc)
	if err != nil {
		return "", fmt.Errorf("billing: decrypt setting %s: %w", key, err)
	}
	return string(plain), nil
}

// StripeKeys resolves the effective Stripe key material: vault-stored
// settings first, env vars as fallback.
func (s *SettingsStore) StripeKeys(ctx context.Context, envSecretKey, envWebhookSecret string) (secretKey, webhookSecret string, fromVault bool, err error) {
	sk, err := s.Get(ctx, SettingStripeSecretKey)
	if err != nil {
		return "", "", false, err
	}
	ws, err := s.Get(ctx, SettingStripeWebhookSecret)
	if err != nil {
		return "", "", false, err
	}
	fromVault = sk != ""
	if sk == "" {
		sk = envSecretKey
	}
	if ws == "" {
		ws = envWebhookSecret
	}
	return sk, ws, fromVault, nil
}

// UpdatedAt returns the last-modified time of a setting (zero when unset).
func (s *SettingsStore) UpdatedAt(ctx context.Context, key string) (time.Time, error) {
	if !s.ready() {
		return time.Time{}, nil
	}
	var t time.Time
	err := s.pool.QueryRow(ctx,
		`select updated_at from suite_billing_settings where key = $1`, key).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("billing: setting metadata %s: %w", key, err)
	}
	return t, nil
}
