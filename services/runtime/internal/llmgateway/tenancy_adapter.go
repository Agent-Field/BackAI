// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"context"

	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
)

// TenancyMirror wraps a *LiteLLMAdmin so it satisfies the
// tenancy.LiteLLMMirror interface.
//
// The two packages keep their own DTOs (llmgateway.KeyConfig vs
// tenancy.LiteLLMKeyConfig) so neither has to import the other for the
// type aliases — this adapter is the one place we translate between
// them. It belongs in llmgateway because llmgateway already imports
// tenancy via this file is the most natural seam: main.go constructs
// the adapter at boot and hands it to tenancy.Manager.WithLiteLLM.
type TenancyMirror struct {
	admin *LiteLLMAdmin
}

// NewTenancyMirror wraps admin for use as a tenancy.LiteLLMMirror.
// admin may be nil — the returned mirror reports Configured() = false.
func NewTenancyMirror(admin *LiteLLMAdmin) *TenancyMirror {
	return &TenancyMirror{admin: admin}
}

// Configured returns true when the underlying admin client is wired.
func (t *TenancyMirror) Configured() bool {
	if t == nil || t.admin == nil {
		return false
	}
	return t.admin.Configured()
}

// VirtualKeysActive reports the cached LiteLLM key-management probe.
func (t *TenancyMirror) VirtualKeysActive() bool {
	if t == nil || t.admin == nil {
		return false
	}
	return t.admin.VirtualKeysActive()
}

func (t *TenancyMirror) VirtualKeysProbeKnown() bool {
	if t == nil || t.admin == nil {
		return false
	}
	mode, _ := t.admin.KeyManagement()
	return mode != KeyMgmtUnknown
}

// ProbeVirtualKeys synchronously refreshes the shared LiteLLM key-management
// probe. tenancy.Manager uses this only when the cached state is still
// unknown, preserving Block 1's local-first rotation behavior.
func (t *TenancyMirror) ProbeVirtualKeys(ctx context.Context) (bool, error) {
	if t == nil || t.admin == nil {
		return false, ErrLiteLLMNotConfigured
	}
	mode, err := t.admin.ProbeKeyManagement(ctx)
	return mode == KeyMgmtVirtualKeys, err
}

// GenerateKey translates the tenancy DTO into the llmgateway DTO and
// proxies the call.
func (t *TenancyMirror) GenerateKey(ctx context.Context, cfg tenancy.LiteLLMKeyConfig) (tenancy.LiteLLMKeyInfo, error) {
	if t == nil || t.admin == nil {
		return tenancy.LiteLLMKeyInfo{}, ErrLiteLLMNotConfigured
	}
	info, err := t.admin.GenerateKey(ctx, fromTenancyKeyConfig(cfg))
	if err != nil {
		return tenancy.LiteLLMKeyInfo{}, err
	}
	return toTenancyKeyInfo(info), nil
}

// UpdateKey proxies to LiteLLMAdmin.UpdateKey.
func (t *TenancyMirror) UpdateKey(ctx context.Context, alias string, cfg tenancy.LiteLLMKeyConfig) error {
	if t == nil || t.admin == nil {
		return ErrLiteLLMNotConfigured
	}
	return t.admin.UpdateKey(ctx, alias, fromTenancyKeyConfig(cfg))
}

// DeleteKey proxies to LiteLLMAdmin.DeleteKey.
func (t *TenancyMirror) DeleteKey(ctx context.Context, alias string) error {
	if t == nil || t.admin == nil {
		return ErrLiteLLMNotConfigured
	}
	return t.admin.DeleteKey(ctx, alias)
}

// KeyInfo proxies to LiteLLMAdmin.KeyInfo.
func (t *TenancyMirror) KeyInfo(ctx context.Context, alias string) (tenancy.LiteLLMKeyInfo, error) {
	if t == nil || t.admin == nil {
		return tenancy.LiteLLMKeyInfo{}, ErrLiteLLMNotConfigured
	}
	info, err := t.admin.KeyInfo(ctx, alias)
	if err != nil {
		return tenancy.LiteLLMKeyInfo{}, err
	}
	return toTenancyKeyInfo(info), nil
}

// KeySpend proxies to LiteLLMAdmin.KeySpend.
func (t *TenancyMirror) KeySpend(ctx context.Context, alias string) (tenancy.LiteLLMSpend, error) {
	if t == nil || t.admin == nil {
		return tenancy.LiteLLMSpend{}, ErrLiteLLMNotConfigured
	}
	s, err := t.admin.KeySpend(ctx, alias)
	if err != nil {
		return tenancy.LiteLLMSpend{}, err
	}
	return tenancy.LiteLLMSpend{TotalUSD: s.TotalUSD, MaxBudgetUSD: s.MaxBudgetUSD}, nil
}

// fromTenancyKeyConfig maps tenancy.LiteLLMKeyConfig -> llmgateway.KeyConfig.
func fromTenancyKeyConfig(cfg tenancy.LiteLLMKeyConfig) KeyConfig {
	return KeyConfig{
		Alias:        cfg.Alias,
		MaxBudgetUSD: cfg.MaxBudgetUSD,
		RPMLimit:     cfg.RPMLimit,
		TPMLimit:     cfg.TPMLimit,
		UserID:       cfg.UserID,
		TenantID:     cfg.TenantID,
		Models:       cfg.Models,
	}
}

// toTenancyKeyInfo maps llmgateway.KeyInfo -> tenancy.LiteLLMKeyInfo.
func toTenancyKeyInfo(info KeyInfo) tenancy.LiteLLMKeyInfo {
	return tenancy.LiteLLMKeyInfo{
		Key:             info.Key,
		Alias:           info.Alias,
		MaxBudgetUSD:    info.MaxBudgetUSD,
		CurrentSpendUSD: info.CurrentSpendUSD,
		RPMLimit:        info.RPMLimit,
		TPMLimit:        info.TPMLimit,
		UserID:          info.UserID,
		TenantID:        info.TenantID,
	}
}

// Compile-time check that TenancyMirror satisfies tenancy.LiteLLMMirror.
var _ tenancy.LiteLLMMirror = (*TenancyMirror)(nil)

// CostSpendReader wraps a *LiteLLMAdmin so it satisfies the
// cost.LiteLLMSpendReader interface. The dashboard's cost summary
// reads live per-key totals from LiteLLM (item #22 — source of
// truth); suite_cost_events is audit-only.
type CostSpendReader struct{ admin *LiteLLMAdmin }

// NewCostSpendReader wraps admin for use as a cost.LiteLLMSpendReader.
func NewCostSpendReader(admin *LiteLLMAdmin) *CostSpendReader {
	return &CostSpendReader{admin: admin}
}

// KeySpend returns the running spend + budget cap for one virtual key.
func (r *CostSpendReader) KeySpend(ctx context.Context, alias string) (cost.Totals, error) {
	if r == nil || r.admin == nil {
		return cost.Totals{}, ErrLiteLLMNotConfigured
	}
	s, err := r.admin.KeySpend(ctx, alias)
	if err != nil {
		return cost.Totals{}, err
	}
	return cost.Totals{SpendUSD: s.TotalUSD, MaxBudgetUSD: s.MaxBudgetUSD}, nil
}

var _ cost.LiteLLMSpendReader = (*CostSpendReader)(nil)
