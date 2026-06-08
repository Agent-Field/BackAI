// SPDX-License-Identifier: Apache-2.0

package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LiteLLMAdmin is a thin HTTP client over LiteLLM's admin API.
//
// LiteLLM Proxy ships a master-key-protected admin surface — distinct
// from the OpenAI-compatible /chat/completions surface that
// LiteLLMProvider talks to. The admin surface owns virtual keys
// (/key/generate, /key/update, /key/delete, /key/info) and spend
// analytics (/spend/keys, /spend/users).
//
// Architecture: AF Stack issues a `suite_api_keys` row + a corresponding
// LiteLLM virtual key in one logical operation. The customer-visible
// shape of the AF Stack token is unchanged; the LiteLLM secret is
// stored encrypted in the secrets vault and used by the runtime when
// it forwards a customer's LLM call upstream. LiteLLM is the source of
// truth for budget remaining and rate-limit state — the runtime never
// has to reconcile.
//
// All admin calls require the LITELLM_MASTER_KEY. That key never leaves
// the runtime container. Failures here are surfaced verbatim to the
// caller (tenancy.Manager) so an issuance can roll back atomically.
type LiteLLMAdmin struct {
	baseURL    string
	masterKey  string
	httpClient *http.Client
}

// NewLiteLLMAdmin constructs an admin client. baseURL is the LiteLLM
// proxy root (e.g. http://litellm:4000); masterKey is the shared
// LITELLM_MASTER_KEY. Either may be empty — empty masterKey is a
// configuration error caught by every method.
func NewLiteLLMAdmin(baseURL, masterKey string) *LiteLLMAdmin {
	return &LiteLLMAdmin{
		baseURL:   strings.TrimRight(baseURL, "/"),
		masterKey: masterKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Configured returns true when both baseURL and masterKey are set.
// Tenancy code uses this to decide whether to attempt LiteLLM mirroring
// at all — in environments without LiteLLM the suite_api_keys row is
// the only thing that gets written, and the gateway falls back to the
// master key (defensive: existing keys keep working).
func (a *LiteLLMAdmin) Configured() bool {
	if a == nil {
		return false
	}
	return a.baseURL != "" && a.masterKey != ""
}

// ─── Wire shapes ─────────────────────────────────────────────────────────

// KeyConfig is the structured input for GenerateKey / UpdateKey.
//
// Fields map directly to LiteLLM's `/key/generate` body. Zero values
// mean "leave unset" — LiteLLM treats unset budget/limits as unlimited.
type KeyConfig struct {
	// Alias is the human-meaningful identifier we attach to the key.
	// We use "af-{tenant_id}-{api_key_id}" so a LiteLLM operator can
	// reconcile by inspection. Required.
	Alias string `json:"key_alias,omitempty"`
	// MaxBudgetUSD is the lifetime spend cap in USD. Zero means
	// unlimited (LiteLLM accepts a missing field).
	MaxBudgetUSD float64 `json:"max_budget,omitempty"`
	// RPMLimit is the per-minute request cap. Zero means unlimited.
	RPMLimit int `json:"rpm_limit,omitempty"`
	// TPMLimit is the per-minute token cap. Zero means unlimited.
	TPMLimit int `json:"tpm_limit,omitempty"`
	// UserID maps the LiteLLM key to an end-user for the /spend/users
	// rollup. Empty means no per-user attribution.
	UserID string `json:"user_id,omitempty"`
	// TenantID is forwarded as team_id so /spend/teams aggregates work
	// in larger deployments. Empty means no team binding.
	TenantID string `json:"team_id,omitempty"`
	// Models is the list of model identifiers this key is allowed to
	// call. Empty means "all models LiteLLM has configured" — caller
	// must pass `["*"]` explicitly if they want LiteLLM's wildcard.
	Models []string `json:"models,omitempty"`
}

// KeyInfo is the response from /key/generate and /key/info.
type KeyInfo struct {
	// Key is the LiteLLM secret value. Only populated by /key/generate;
	// /key/info returns metadata without the secret. Treat as a secret —
	// never log, store encrypted only.
	Key string `json:"key,omitempty"`
	// Alias mirrors KeyConfig.Alias.
	Alias string `json:"key_alias,omitempty"`
	// MaxBudgetUSD is what LiteLLM stored. May differ slightly from the
	// input if LiteLLM rounds or normalises.
	MaxBudgetUSD *float64 `json:"max_budget,omitempty"`
	// CurrentSpendUSD is LiteLLM's running total against MaxBudgetUSD.
	// Subtract for "remaining"; nil means LiteLLM didn't include it.
	CurrentSpendUSD *float64 `json:"spend,omitempty"`
	// RPMLimit / TPMLimit echo back the input. Nil means LiteLLM
	// surfaced no value (treat as unlimited).
	RPMLimit *int `json:"rpm_limit,omitempty"`
	TPMLimit *int `json:"tpm_limit,omitempty"`
	// UserID / TenantID echo back the input.
	UserID   string `json:"user_id,omitempty"`
	TenantID string `json:"team_id,omitempty"`
	// Models echoes the input.
	Models []string `json:"models,omitempty"`
}

// Spend is the normalised payload of /spend/keys?key=... and
// /spend/users?user_id=....
//
// LiteLLM's raw shape varies by version: sometimes a flat object,
// sometimes wrapped in `{ "info": [...] }` for multi-tenant rollups.
// We collapse to the dashboard-relevant fields and let callers ignore
// the rest.
type Spend struct {
	// TotalUSD is the running cost the key (or user) has accumulated.
	// Always populated; defaults to 0 when LiteLLM returns nothing.
	TotalUSD float64 `json:"spend"`
	// MaxBudgetUSD echoes the key's configured budget for convenience
	// — saves the caller a second /key/info round-trip. nil = LiteLLM
	// didn't include it (treat as unlimited).
	MaxBudgetUSD *float64 `json:"max_budget,omitempty"`
}

// RemainingUSD computes max_budget - spend. Returns (0, false) when
// no budget is configured.
func (s Spend) RemainingUSD() (float64, bool) {
	if s.MaxBudgetUSD == nil {
		return 0, false
	}
	r := *s.MaxBudgetUSD - s.TotalUSD
	if r < 0 {
		r = 0
	}
	return r, true
}

// ─── Errors ──────────────────────────────────────────────────────────────

// ErrLiteLLMNotConfigured is returned when the admin client has no base
// URL or master key. Tenancy treats this as a hard error during issuance
// when LiteLLM mirroring is required, and as a soft skip when reading
// spend (the dashboard renders em-dashes).
var ErrLiteLLMNotConfigured = errors.New("litellm admin: base URL or master key not configured")

// ErrLiteLLMKeyNotFound is returned when /key/info or /spend/keys
// reports the alias as missing. Callers should treat as "no live data"
// rather than a hard failure when polling for dashboards.
var ErrLiteLLMKeyNotFound = errors.New("litellm admin: key not found")

// AdminError wraps a non-2xx response from LiteLLM so the caller can
// inspect both the status and the upstream body.
type AdminError struct {
	StatusCode int
	Endpoint   string
	Body       string
}

func (e *AdminError) Error() string {
	return fmt.Sprintf("litellm admin: %s returned %d: %s",
		e.Endpoint, e.StatusCode, truncateForLog(e.Body, 256))
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ─── Operations ──────────────────────────────────────────────────────────

// GenerateKey creates a new LiteLLM virtual key. On success returns the
// KeyInfo with Key populated — the caller MUST store it encrypted
// (e.g. via secrets.Vault) and never log it.
func (a *LiteLLMAdmin) GenerateKey(ctx context.Context, cfg KeyConfig) (KeyInfo, error) {
	if !a.Configured() {
		return KeyInfo{}, ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(cfg.Alias) == "" {
		return KeyInfo{}, errors.New("litellm admin: alias required")
	}
	var out KeyInfo
	if err := a.post(ctx, "/key/generate", cfg, &out); err != nil {
		return KeyInfo{}, err
	}
	if out.Alias == "" {
		// LiteLLM occasionally omits the alias in the response — fill
		// from the input so callers can reconcile by alias unconditionally.
		out.Alias = cfg.Alias
	}
	return out, nil
}

// UpdateKey patches an existing LiteLLM virtual key by alias. Only
// non-zero fields in cfg are forwarded; the alias is required and
// identifies which key to mutate.
func (a *LiteLLMAdmin) UpdateKey(ctx context.Context, alias string, cfg KeyConfig) error {
	if !a.Configured() {
		return ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(alias) == "" {
		return errors.New("litellm admin: alias required")
	}
	cfg.Alias = alias
	// /key/update returns an opaque envelope; we don't need any field
	// from it — a 2xx is sufficient signal.
	var sink map[string]any
	return a.post(ctx, "/key/update", cfg, &sink)
}

// DeleteKey revokes a LiteLLM virtual key by alias.
func (a *LiteLLMAdmin) DeleteKey(ctx context.Context, alias string) error {
	if !a.Configured() {
		return ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(alias) == "" {
		return errors.New("litellm admin: alias required")
	}
	// LiteLLM's /key/delete accepts either `keys` (list of aliases) or
	// `key_aliases`. We use `key_aliases` — the more recent of the two
	// shapes — and fall back to `keys` if the proxy returns 400.
	body := map[string]any{"key_aliases": []string{alias}}
	var sink map[string]any
	if err := a.post(ctx, "/key/delete", body, &sink); err == nil {
		return nil
	} else {
		var ae *AdminError
		if !errors.As(err, &ae) || ae.StatusCode != http.StatusBadRequest {
			return err
		}
		// Older proxy: fall back to `keys`.
		body = map[string]any{"keys": []string{alias}}
		return a.post(ctx, "/key/delete", body, &sink)
	}
}

// KeyInfo reads metadata for a single virtual key by alias. Returns
// ErrLiteLLMKeyNotFound when LiteLLM reports the alias as missing.
func (a *LiteLLMAdmin) KeyInfo(ctx context.Context, alias string) (KeyInfo, error) {
	if !a.Configured() {
		return KeyInfo{}, ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(alias) == "" {
		return KeyInfo{}, errors.New("litellm admin: alias required")
	}
	q := url.Values{}
	q.Set("key_alias", alias)
	var out KeyInfo
	if err := a.get(ctx, "/key/info", q, &out); err != nil {
		return KeyInfo{}, err
	}
	if out.Alias == "" && out.Key == "" {
		// Defensive: LiteLLM returns an empty envelope when the alias
		// isn't found (rather than 404 in some versions). Treat empty
		// alias + empty key as a miss.
		return KeyInfo{}, ErrLiteLLMKeyNotFound
	}
	if out.Alias == "" {
		out.Alias = alias
	}
	return out, nil
}

// KeySpend reads cumulative spend for a single key by alias. Wraps
// /spend/keys.
func (a *LiteLLMAdmin) KeySpend(ctx context.Context, alias string) (Spend, error) {
	if !a.Configured() {
		return Spend{}, ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(alias) == "" {
		return Spend{}, errors.New("litellm admin: alias required")
	}
	q := url.Values{}
	q.Set("key_alias", alias)
	return a.readSpend(ctx, "/spend/keys", q)
}

// UserSpend reads cumulative spend across all virtual keys mapped to
// a user. Wraps /spend/users.
func (a *LiteLLMAdmin) UserSpend(ctx context.Context, userID string) (Spend, error) {
	if !a.Configured() {
		return Spend{}, ErrLiteLLMNotConfigured
	}
	if strings.TrimSpace(userID) == "" {
		return Spend{}, errors.New("litellm admin: user_id required")
	}
	q := url.Values{}
	q.Set("user_id", userID)
	return a.readSpend(ctx, "/spend/users", q)
}

// ─── HTTP plumbing ───────────────────────────────────────────────────────

// readSpend handles the two LiteLLM /spend shapes:
//  1. A single object: {"spend": 1.23, "max_budget": 10}
//  2. A wrapper with an array: {"info": [{"spend": 1.23, ...}]} — common
//     for /spend/users when a user has multiple keys.
//
// We collapse to a single Spend (sum across array). Missing fields
// default to zero / nil.
func (a *LiteLLMAdmin) readSpend(ctx context.Context, path string, q url.Values) (Spend, error) {
	body, status, err := a.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return Spend{}, err
	}
	if status == http.StatusNotFound {
		return Spend{}, ErrLiteLLMKeyNotFound
	}
	if status >= 400 {
		return Spend{}, &AdminError{StatusCode: status, Endpoint: path, Body: string(body)}
	}

	// Try the wrapper shape first.
	var wrapped struct {
		Info []Spend `json:"info"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Info) > 0 {
		out := Spend{}
		var maxBudget *float64
		for _, s := range wrapped.Info {
			out.TotalUSD += s.TotalUSD
			if s.MaxBudgetUSD != nil {
				v := *s.MaxBudgetUSD
				if maxBudget == nil {
					maxBudget = &v
				} else {
					sum := *maxBudget + v
					maxBudget = &sum
				}
			}
		}
		out.MaxBudgetUSD = maxBudget
		return out, nil
	}

	// Fall back to the flat shape.
	var flat Spend
	if err := json.Unmarshal(body, &flat); err != nil {
		return Spend{}, fmt.Errorf("litellm admin: decode spend response: %w", err)
	}
	return flat, nil
}

func (a *LiteLLMAdmin) get(ctx context.Context, path string, q url.Values, out any) error {
	body, status, err := a.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return ErrLiteLLMKeyNotFound
	}
	if status >= 400 {
		return &AdminError{StatusCode: status, Endpoint: path, Body: string(body)}
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("litellm admin: decode %s: %w", path, err)
	}
	return nil
}

func (a *LiteLLMAdmin) post(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("litellm admin: encode %s: %w", path, err)
	}
	respBody, status, err := a.do(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return ErrLiteLLMKeyNotFound
	}
	if status >= 400 {
		return &AdminError{StatusCode: status, Endpoint: path, Body: string(respBody)}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("litellm admin: decode %s: %w", path, err)
	}
	return nil
}

// do is the single HTTP entry point. Builds the URL, sets the master
// key, executes, reads the body (size-bounded), returns body + status.
func (a *LiteLLMAdmin) do(
	ctx context.Context,
	method, path string,
	q url.Values,
	body []byte,
) ([]byte, int, error) {
	if a.baseURL == "" {
		return nil, 0, ErrLiteLLMNotConfigured
	}
	u := a.baseURL + path
	if len(q) > 0 {
		u = u + "?" + q.Encode()
	}
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("litellm admin: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.masterKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("litellm admin: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return respBody, resp.StatusCode, nil
}
