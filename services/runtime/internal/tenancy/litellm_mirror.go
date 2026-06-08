// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LiteLLMMirror is the subset of llmgateway.LiteLLMAdmin the tenancy
// Manager needs. Defined as an interface here so:
//   - tenancy doesn't import llmgateway (avoids an import cycle and
//     keeps the package's blast radius small),
//   - tests can stub out the mirror without spinning up a fake HTTP
//     server.
//
// The methods mirror llmgateway.LiteLLMAdmin one-for-one. Field shapes
// (KeyConfig / KeyInfo / Spend) live in this package as small DTOs so
// callers don't pull in llmgateway either.
type LiteLLMMirror interface {
	// Configured returns true when the mirror is wired (base URL +
	// master key). When false, IssueAPIKey silently skips mirroring
	// and the LLM gateway falls back to LITELLM_MASTER_KEY at request
	// time.
	Configured() bool
	GenerateKey(ctx context.Context, cfg LiteLLMKeyConfig) (LiteLLMKeyInfo, error)
	UpdateKey(ctx context.Context, alias string, cfg LiteLLMKeyConfig) error
	DeleteKey(ctx context.Context, alias string) error
	KeyInfo(ctx context.Context, alias string) (LiteLLMKeyInfo, error)
	KeySpend(ctx context.Context, alias string) (LiteLLMSpend, error)
}

// SecretSink is the subset of secrets.Vault the tenancy Manager needs
// to stash the LiteLLM secret. Defined here so tenancy doesn't import
// secrets either (same isolation argument as LiteLLMMirror).
type SecretSink interface {
	Put(ctx context.Context, tenantID, key string, in SecretPutInput) error
	Delete(ctx context.Context, tenantID, key string) error
	Get(ctx context.Context, tenantID, key string) ([]byte, error)
}

// SecretPutInput is the minimal field set Manager hands to the sink.
// Mirrors secrets.PutInput; we duplicate to avoid the import.
type SecretPutInput struct {
	Value       string
	Description string
}

// LiteLLMKeyConfig mirrors llmgateway.KeyConfig. We duplicate so
// tenancy doesn't import llmgateway.
type LiteLLMKeyConfig struct {
	Alias        string
	MaxBudgetUSD float64
	RPMLimit     int
	TPMLimit     int
	UserID       string
	TenantID     string
	Models       []string
}

// LiteLLMKeyInfo mirrors llmgateway.KeyInfo. Key is the LiteLLM secret
// returned by /key/generate — store encrypted only.
type LiteLLMKeyInfo struct {
	Key             string
	Alias           string
	MaxBudgetUSD    *float64
	CurrentSpendUSD *float64
	RPMLimit        *int
	TPMLimit        *int
	UserID          string
	TenantID        string
}

// LiteLLMSpend mirrors llmgateway.Spend.
type LiteLLMSpend struct {
	TotalUSD     float64
	MaxBudgetUSD *float64
}

// RemainingUSD mirrors llmgateway.Spend.RemainingUSD.
func (s LiteLLMSpend) RemainingUSD() (float64, bool) {
	if s.MaxBudgetUSD == nil {
		return 0, false
	}
	r := *s.MaxBudgetUSD - s.TotalUSD
	if r < 0 {
		r = 0
	}
	return r, true
}

// WithLiteLLM attaches a LiteLLM mirror + secrets sink to the Manager.
// Both are optional: when nil the Manager keeps working in legacy mode
// (suite_api_keys insert only, no LiteLLM mirroring, no per-tenant
// secret).
//
// Call once at boot — Manager methods read the fields under no lock,
// and concurrent mutation is not supported.
func (m *Manager) WithLiteLLM(mirror LiteLLMMirror, secrets SecretSink) *Manager {
	if m == nil {
		return nil
	}
	m.litellm = mirror
	m.secrets = secrets
	return m
}

// LiteLLMMirror returns the wired mirror (or nil).
func (m *Manager) LiteLLMMirror() LiteLLMMirror {
	if m == nil {
		return nil
	}
	return m.litellm
}

// SecretSink returns the wired sink (or nil).
func (m *Manager) SecretSink() SecretSink {
	if m == nil {
		return nil
	}
	return m.secrets
}

// LiteLLMKeyForAPIKey returns the LiteLLM secret stored for the given
// AF Stack api_key_id (decrypts via secrets vault). Returns ("", nil)
// when no per-tenant secret exists — callers should fall back to the
// master key for the LLM gateway call.
//
// The caller (LLM gateway) is expected to cache the result for the
// life of one request — every chat/completions call hits this method
// once, so a fresh DB / vault round-trip per request is fine
// (10ms-class).
func (m *Manager) LiteLLMKeyForAPIKey(ctx context.Context, tenantID, apiKeyID string) (string, error) {
	if m == nil || m.secrets == nil {
		return "", nil
	}
	if tenantID == "" || apiKeyID == "" {
		return "", nil
	}
	plain, err := m.secrets.Get(ctx, tenantID, LiteLLMSecretKey(apiKeyID))
	if err != nil {
		// Vault returns its own not-found sentinel; we can't import
		// secrets here so we test by error string. The fallback path is
		// "return empty — gateway will use master key".
		if isSecretNotFoundErr(err) {
			return "", nil
		}
		return "", err
	}
	return string(plain), nil
}

// LiteLLMSecretKey is the canonical key under which a LiteLLM secret
// lives in the secrets vault. Exported so the LLM gateway can reuse the
// same path without re-deriving it.
//
// The vault's validateKey rejects ':', so we use '/' to namespace.
func LiteLLMSecretKey(apiKeyID string) string {
	return "litellm/key/" + apiKeyID
}

// LiteLLMAliasFor returns the alias we send to LiteLLM at issuance
// time. We use a deterministic shape so a LiteLLM operator can
// reconcile by inspection ("af-{tenant_id}-{api_key_id}").
//
// Tenant ID and API key ID are UUIDs which include hyphens — LiteLLM
// accepts arbitrary alias strings so we don't need to escape anything.
func LiteLLMAliasFor(tenantID, apiKeyID string) string {
	return "af-" + tenantID + "-" + apiKeyID
}

// hashLiteLLMSecret hashes the LiteLLM secret with SHA-256. The hash
// goes in suite_api_keys.litellm_key_hash so operators can verify
// without ever seeing the secret in the row.
func hashLiteLLMSecret(secret string) string {
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// isSecretNotFoundErr is a string-match against the secrets package
// sentinel. Avoiding the import keeps tenancy small; the cost is a
// brittle string check, mitigated by the fact that the sentinel is
// stable.
func isSecretNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "secret not found") ||
		strings.Contains(msg, "secrets: not found")
}

// issueLiteLLMKey mirrors the AF Stack api key to a LiteLLM virtual
// key + stashes the LiteLLM secret in the vault.
//
// Returns the alias + hash + secret so the caller can persist the
// public bits on the suite_api_keys row.
//
// On any failure inside this function the caller is responsible for
// rolling back the suite_api_keys row insert — atomicity matters
// because a row without its LiteLLM mapping is a misconfigured key
// that would silently fall back to the master key at request time.
func (m *Manager) issueLiteLLMKey(
	ctx context.Context,
	tenantID, apiKeyID string,
	in IssueAPIKeyInput,
) (alias, hash string, err error) {
	if m.litellm == nil || !m.litellm.Configured() {
		return "", "", nil
	}
	alias = LiteLLMAliasFor(tenantID, apiKeyID)
	cfg := LiteLLMKeyConfig{
		Alias:    alias,
		UserID:   in.CreatedBy,
		TenantID: tenantID,
	}
	if in.BudgetMaxUSD != nil {
		cfg.MaxBudgetUSD = *in.BudgetMaxUSD
	}
	if in.RateLimitRPM != nil {
		cfg.RPMLimit = *in.RateLimitRPM
	}
	if in.RateLimitTPM != nil {
		cfg.TPMLimit = *in.RateLimitTPM
	}

	// Bounded timeout — LiteLLM should answer in well under 5s; if it
	// doesn't, abort and let the caller roll back rather than hold an
	// HTTP request open indefinitely.
	genCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := m.litellm.GenerateKey(genCtx, cfg)
	if err != nil {
		return "", "", fmt.Errorf("tenancy: litellm GenerateKey: %w", err)
	}
	if info.Key == "" {
		return "", "", errors.New("tenancy: litellm GenerateKey returned empty key")
	}

	// Store the secret encrypted. If the vault isn't wired we still
	// persist alias/hash so the row carries a record of the mapping —
	// but the LLM gateway won't be able to use a per-tenant key. This
	// is a degraded-but-functional configuration (the gateway falls
	// back to the master key); we log it loudly.
	if m.secrets != nil {
		if putErr := m.secrets.Put(ctx, tenantID, LiteLLMSecretKey(apiKeyID), SecretPutInput{
			Value:       info.Key,
			Description: "LiteLLM virtual key for api_key=" + apiKeyID,
		}); putErr != nil {
			// Best-effort rollback of the LiteLLM key — we can still
			// surface the original error.
			_ = m.litellm.DeleteKey(context.Background(), alias)
			return "", "", fmt.Errorf("tenancy: vault Put litellm secret: %w", putErr)
		}
	} else {
		m.log.Warn("tenancy: secrets sink not configured — LiteLLM key created but secret unstored; LLM gateway will fall back to master key",
			"api_key_id", apiKeyID,
			"alias", alias,
		)
	}
	return alias, hashLiteLLMSecret(info.Key), nil
}

// revokeLiteLLMKey deletes the LiteLLM virtual key + secret. Failures
// are logged but never propagated — once an AF Stack key is revoked
// the LLM gateway will reject the customer's call long before LiteLLM
// sees it, so a stale LiteLLM key is a leak (LiteLLM admin can clean
// up), not a security hole.
func (m *Manager) revokeLiteLLMKey(ctx context.Context, tenantID, apiKeyID, alias string) {
	if m == nil {
		return
	}
	if m.litellm != nil && m.litellm.Configured() && alias != "" {
		if err := m.litellm.DeleteKey(ctx, alias); err != nil {
			m.log.Warn("tenancy: litellm DeleteKey failed",
				"alias", alias, "error", err)
		}
	}
	if m.secrets != nil && apiKeyID != "" {
		if err := m.secrets.Delete(ctx, tenantID, LiteLLMSecretKey(apiKeyID)); err != nil && !isSecretNotFoundErr(err) {
			m.log.Warn("tenancy: vault Delete litellm secret failed",
				"api_key_id", apiKeyID, "error", err)
		}
	}
}
