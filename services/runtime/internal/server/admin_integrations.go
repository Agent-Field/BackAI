// SPDX-License-Identifier: Apache-2.0

// admin_integrations.go — operator-scoped Integrations settings API.
//
// This is how an operator enters adapter credentials (Resend key, Slack
// webhook, Twilio SID/token, FCM token, storage/secrets/llm remote URL +
// token, …) from the dashboard instead of editing env vars. Credentials
// are persisted in the per-tenant secrets vault; the values NEVER leave
// the runtime — the API only reports whether each field is set plus an
// optional masked fingerprint.
//
// Vault key convention (READ THIS — the boot-time wiring that resolves
// these creds MUST use the identical shape):
//
//	integration/{slot}/{fieldName}
//
// NOTE ON THE SEPARATOR: the design intent was "integration:{slot}:{field}"
// but the vault's validateKey charset is [A-Za-z0-9_-./] and REJECTS ':'
// (secrets.ErrInvalidKey). We therefore namespace with '/', which is both
// allowed and unambiguous (field names contain '_' but never '/'). Anyone
// resolving these keys at boot must read "integration/{slot}/{field}".
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/rbac"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// integrationKeyPrefix is the first path segment of every integration
// vault key.
const integrationKeyPrefix = "integration"

// integrationFields is the SINGLE SOURCE OF TRUTH for which credential
// fields each configurable adapter slot exposes to the operator
// dashboard. The GET/PUT handlers are generic over this map — to add a
// field to a slot (or a whole new slot), edit ONLY this map. Field names
// are the JSON keys the dashboard sends and the exact {fieldName} written
// into the vault key.
var integrationFields = map[string][]string{
	// Notification channels (email / chat / sms / push). A sibling agent
	// owns the channel semantics; these are the credential inputs.
	"notifications": {
		"resend_api_key",     // Resend (email) API key
		"slack_webhook_url",  // Slack incoming-webhook URL
		"twilio_account_sid", // Twilio (SMS) account SID
		"twilio_auth_token",  // Twilio auth token
		"twilio_from_number", // Twilio sending number (E.164)
		"fcm_project_id",     // Firebase Cloud Messaging (push) project id
		"fcm_access_token",   // FCM access token
	},
	// Remote-adapter shims: URL + bearer token for the out-of-process
	// backend that fronts each of these slots.
	"storage": {"remote_url", "remote_token"},
	"secrets": {"remote_url", "remote_token"},
	"llm":     {"remote_url", "remote_token"},
	// Sandbox (code execution). Adapter selection stays env-based
	// (AF_STACK_SANDBOX_ADAPTER); these are the credentials the e2b and
	// remote adapters consume, resolved by newSandbox at boot.
	"sandbox": {"e2b_api_key", "e2b_base_url", "remote_url", "remote_token"},
	// Browser tool backends: self-hosted sidecar URL, hosted-provider
	// API keys (Steel, Browserbase), or a raw CDP/playwright websocket
	// endpoint (Browserless et al). allow_private ("true"/"false")
	// re-permits loopback/RFC-1918 endpoints for compose-network
	// sidecars. Adapter selection stays env-based (AF_STACK_TOOL_BROWSER).
	"browser": {
		"browser_use_url",
		"steel_api_key",
		"browserbase_api_key",
		"browserbase_project_id",
		"playwright_endpoint",
		"allow_private",
	},
	// OAuth-on-behalf-of-user provider slots are registered dynamically in
	// init() from oauth.AllProviderNames so the slot set stays in lockstep
	// with the shipped adapters. Each slot exposes client_id + client_secret.
}

// oauthIntegrationSlotPrefix namespaces the per-provider OAuth credential
// slots ("oauth_google", "oauth_github", …) so they sit alongside the
// adapter slots without colliding. The oauth.Factory resolver
// (newOAuthCredentialResolver) maps a provider name back onto this slot.
const oauthIntegrationSlotPrefix = "oauth_"

// oauthIntegrationSlot returns the integration slot name for an OAuth
// provider (e.g. "google" → "oauth_google").
func oauthIntegrationSlot(provider string) string {
	return oauthIntegrationSlotPrefix + provider
}

// oauthIntegrationFields are the credential inputs every OAuth provider
// slot exposes to the operator dashboard.
var oauthIntegrationFields = []string{"client_id", "client_secret"}

func init() {
	// Register one integration slot per known OAuth provider so operators
	// can enter Google/GitHub (etc.) client id + secret from the dashboard.
	for _, name := range oauth.AllProviderNames {
		integrationFields[oauthIntegrationSlot(name)] = append([]string(nil), oauthIntegrationFields...)
	}
}

// newOAuthCredentialResolver returns an oauth.CredentialResolver backed by
// the integration vault slots for the default (single) operator tenant.
// Resolution is vault-first; the env fallback (OAUTH_<NAME>_*) lives in the
// oauth.Factory. Reading the store lazily on every call (rather than
// capturing a snapshot) is what makes an operator-entered credential take
// effect on the next OAuth request WITHOUT a runtime restart.
func (s *Server) newOAuthCredentialResolver() oauth.CredentialResolver {
	return func(provider, field string) (string, bool) {
		store := s.integrationStore()
		if store == nil {
			return "", false
		}
		key := integrationVaultKey(oauthIntegrationSlot(provider), field)
		v, err := store.Get(context.Background(), s.defaultTenant(nil), key)
		if err != nil {
			return "", false
		}
		val := strings.TrimSpace(string(v))
		return val, val != ""
	}
}

// integrationVaultKey builds the vault key for a (slot, field). See the
// package doc for the convention and why the separator is '/'.
func integrationVaultKey(slot, field string) string {
	return integrationKeyPrefix + "/" + slot + "/" + field
}

// ─── response shapes (downstream dashboard MUST match these) ──────────────

// integrationProvider groups a slot's fields by the concrete backend
// they configure. The dashboard renders one provider at a time behind a
// dropdown instead of a wall of every field for every backend. A field
// may appear under more than one provider (same vault key either way).
type integrationProvider struct {
	ID     string
	Label  string
	Fields []string
}

// integrationProviders maps slots with multiple selectable backends to
// their provider groups. Slots absent from this map render as a single
// implicit provider carrying all of the slot's fields (no dropdown).
// Every field named here MUST exist in integrationFields for the slot —
// pinned by TestAdminIntegrationsProvidersConsistent.
var integrationProviders = map[string][]integrationProvider{
	"notifications": {
		{ID: "resend", Label: "Resend (email)", Fields: []string{"resend_api_key"}},
		{ID: "slack", Label: "Slack webhook", Fields: []string{"slack_webhook_url"}},
		{ID: "twilio", Label: "Twilio (SMS)", Fields: []string{"twilio_account_sid", "twilio_auth_token", "twilio_from_number"}},
		{ID: "fcm", Label: "Firebase push (FCM)", Fields: []string{"fcm_project_id", "fcm_access_token"}},
	},
	"sandbox": {
		{ID: "e2b", Label: "E2B (hosted)", Fields: []string{"e2b_api_key", "e2b_base_url"}},
		{ID: "remote", Label: "Remote sidecar", Fields: []string{"remote_url", "remote_token"}},
	},
	"browser": {
		{ID: "browser-use", Label: "Self-hosted sidecar", Fields: []string{"browser_use_url", "allow_private"}},
		{ID: "steel", Label: "Steel (hosted)", Fields: []string{"steel_api_key"}},
		{ID: "browserbase", Label: "Browserbase (hosted)", Fields: []string{"browserbase_api_key", "browserbase_project_id"}},
		{ID: "playwright", Label: "CDP / Playwright endpoint", Fields: []string{"playwright_endpoint", "allow_private"}},
	},
}

// integrationFieldDefaults records the value a field falls back to when
// left blank, so the dashboard can render it as explicitly optional
// ("defaults to …") instead of an empty required-looking input. Keep in
// sync with the consuming factory's actual fallback.
var integrationFieldDefaults = map[string]string{
	"e2b_base_url":  "https://api.e2b.dev", // e2b adapter default (sandbox/adapters/e2b)
	"allow_private": "false",               // SSRF guard blocks private endpoints unless enabled
}

// integrationFieldNotes carries free-text optionality guidance for fields
// that are optional without a literal default value. Fields with neither
// a default nor a note render as required. Every field's classification
// must match what the consuming adapter actually enforces.
var integrationFieldNotes = map[string]string{
	// Browserbase infers the project from a single-project API key.
	"browserbase_project_id": "Optional — inferred from the API key when left blank.",
	// The remote-adapter sidecar protocol enforces bearer auth only when
	// the sidecar itself is configured with a token.
	"remote_token": "Optional — only needed when the remote adapter requires bearer auth.",
}

// integrationFieldKinds marks fields that are NOT secrets (endpoints,
// flags, plain identifiers) so the dashboard can render them as normal
// text inputs instead of masked password fields. Absent = secret.
// Keyed by field name; only list fields that are safe to display.
var integrationFieldKinds = map[string]string{
	"e2b_base_url":           "text",
	"browser_use_url":        "text",
	"playwright_endpoint":    "text",
	"browserbase_project_id": "text",
	"allow_private":          "text",
}

// integrationFieldStatus reports one credential field. It NEVER carries a
// raw secret value — only whether it is set and an optional masked hint.
// Kind is "text" for non-secret fields (rendered unmasked); empty means
// secret. Default, when non-empty, is the value the runtime falls back
// to for a blank field — the UI renders such fields as optional.
type integrationFieldStatus struct {
	Name    string `json:"name"`
	Set     bool   `json:"set"`
	Hint    string `json:"hint"`
	Kind    string `json:"kind,omitempty"`
	Default string `json:"default,omitempty"`
	// Note is free-text optionality guidance for fields optional without
	// a literal default. Empty Default + empty Note = required field.
	Note string `json:"note,omitempty"`
}

// integrationProviderStatus is one selectable backend inside a slot,
// carrying just that backend's field statuses.
type integrationProviderStatus struct {
	ID     string                   `json:"id"`
	Label  string                   `json:"label"`
	Fields []integrationFieldStatus `json:"fields"`
}

// integrationSlotStatus is the per-slot status object returned by GET
// (once per slot) and PUT (for the mutated slot). Fields is the flat
// union (kept for compatibility); Providers groups the same statuses by
// backend for the dropdown UI.
type integrationSlotStatus struct {
	Slot          string                      `json:"slot"`
	ActiveAdapter string                      `json:"activeAdapter"`
	Fields        []integrationFieldStatus    `json:"fields"`
	Providers     []integrationProviderStatus `json:"providers"`
}

// integrationsListResponse is the GET envelope.
type integrationsListResponse struct {
	Integrations []integrationSlotStatus `json:"integrations"`
}

// putIntegrationInput is the PUT body.
type putIntegrationInput struct {
	Credentials map[string]string `json:"credentials"`
}

// ─── secrets-store seam ───────────────────────────────────────────────────

// integrationStoreHook lets tests substitute an in-memory secrets.Store so
// the handlers can be exercised without a live Postgres vault. Production
// leaves it nil and the handlers use the wired *secrets.Vault.
var integrationStoreHook func(*Server) secrets.Store

// integrationStore returns the secrets.Store backing the integration
// credentials, or nil when no vault is wired. Guards against the typed-nil
// interface trap (a nil *secrets.Vault boxed into a non-nil Store).
func (s *Server) integrationStore() secrets.Store {
	if integrationStoreHook != nil {
		return integrationStoreHook(s)
	}
	if s.secrets == nil {
		return nil
	}
	return s.secrets
}

// integrationsUnavailable writes a 503 (mirroring the secrets surface) when
// no vault is configured and returns true.
func (s *Server) integrationsUnavailable(w http.ResponseWriter) bool {
	if s.integrationStore() == nil {
		writeError(w, http.StatusServiceUnavailable, "SECRETS_NOT_CONFIGURED",
			"secrets vault is not configured on this runtime", nil)
		return true
	}
	return false
}

// ─── helpers ──────────────────────────────────────────────────────────────

// maskHint returns a redacted fingerprint of a secret value — the first few
// and last few characters with the middle elided. It NEVER returns the raw
// value; values too short to keep meaningful bytes hidden return "".
func maskHint(v string) string {
	const head, tail = 3, 4
	// Require at least 3 hidden characters between the revealed ends.
	if len(v) < head+tail+3 {
		return ""
	}
	return v[:head] + "..." + v[len(v)-tail:]
}

// integrationActiveAdapter returns the name of the active adapter for a slot
// as reported by the adapter registry, or "" if unknown / no registry.
func (s *Server) integrationActiveAdapter(ctx context.Context, slot string) string {
	if s.adapterRegistry == nil {
		return ""
	}
	for _, v := range s.adapterRegistry.List(ctx).Slots {
		if v.Slot == slot {
			return v.Active.Name
		}
	}
	return ""
}

// buildSlotStatus assembles the status object for one slot by probing the
// vault for each field. Existence is proven via GetMetadata (no decrypt);
// the value is decrypted only to compute a masked hint and is discarded.
func (s *Server) buildSlotStatus(ctx context.Context, tenantID, slot string, store secrets.Store) (integrationSlotStatus, error) {
	fields := integrationFields[slot]
	out := integrationSlotStatus{
		Slot:          slot,
		ActiveAdapter: s.integrationActiveAdapter(ctx, slot),
		Fields:        make([]integrationFieldStatus, 0, len(fields)),
	}
	byName := make(map[string]integrationFieldStatus, len(fields))
	for _, f := range fields {
		key := integrationVaultKey(slot, f)
		fs := integrationFieldStatus{
			Name:    f,
			Kind:    integrationFieldKinds[f],
			Default: integrationFieldDefaults[f],
			Note:    integrationFieldNotes[f],
		}
		if _, err := store.GetMetadata(ctx, tenantID, key); err != nil {
			if !errors.Is(err, secrets.ErrSecretNotFound) {
				return integrationSlotStatus{}, err
			}
		} else {
			fs.Set = true
			if plain, err := store.Get(ctx, tenantID, key); err == nil {
				fs.Hint = maskHint(string(plain))
			}
		}
		byName[f] = fs
		out.Fields = append(out.Fields, fs)
	}

	// Group the same statuses by provider. Slots without an explicit
	// provider map get one implicit provider holding every field, so the
	// dashboard can treat providers as the universal shape (dropdown only
	// when there is more than one).
	provs := integrationProviders[slot]
	if len(provs) == 0 {
		provs = []integrationProvider{{ID: slot, Label: "", Fields: fields}}
	}
	out.Providers = make([]integrationProviderStatus, 0, len(provs))
	for _, p := range provs {
		ps := integrationProviderStatus{ID: p.ID, Label: p.Label, Fields: make([]integrationFieldStatus, 0, len(p.Fields))}
		for _, f := range p.Fields {
			ps.Fields = append(ps.Fields, byName[f])
		}
		out.Providers = append(out.Providers, ps)
	}
	return out, nil
}

// ─── handlers ─────────────────────────────────────────────────────────────

// handleAdminListIntegrations serves GET /api/v1/admin/integrations.
func (s *Server) handleAdminListIntegrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if s.integrationsUnavailable(w) {
		return
	}
	store := s.integrationStore()
	tenantID := s.defaultTenant(r)

	slots := make([]string, 0, len(integrationFields))
	for slot := range integrationFields {
		slots = append(slots, slot)
	}
	sort.Strings(slots)

	resp := integrationsListResponse{
		Integrations: make([]integrationSlotStatus, 0, len(slots)),
	}
	for _, slot := range slots {
		st, err := s.buildSlotStatus(ctx, tenantID, slot, store)
		if err != nil {
			s.log.Error("integrations: build status failed", "slot", slot, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL",
				"failed to read integration status", nil)
			return
		}
		resp.Integrations = append(resp.Integrations, st)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminPutIntegration serves PUT /api/v1/admin/integrations/{slot}.
func (s *Server) handleAdminPutIntegration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slot := r.PathValue("slot")

	fields, ok := integrationFields[slot]
	if !ok {
		writeError(w, http.StatusBadRequest, "INTEGRATION_SLOT_UNKNOWN",
			"unknown integration slot", map[string]any{"slot": slot})
		return
	}
	if s.integrationsUnavailable(w) {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return
	}
	var in putIntegrationInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body", nil)
		return
	}

	allowed := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		allowed[f] = struct{}{}
	}

	store := s.integrationStore()
	tenantID := s.defaultTenant(r)

	for field, value := range in.Credentials {
		if _, ok := allowed[field]; !ok {
			writeError(w, http.StatusBadRequest, "INTEGRATION_FIELD_UNKNOWN",
				"unknown credential field for slot",
				map[string]any{"slot": slot, "field": field})
			return
		}
		key := integrationVaultKey(slot, field)
		if value == "" {
			// Empty value clears the credential. Deleting a missing key is a no-op.
			if err := store.Delete(ctx, tenantID, key); err != nil &&
				!errors.Is(err, secrets.ErrSecretNotFound) {
				s.log.Error("integrations: delete failed",
					"slot", slot, "field", field, "error", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL",
					"failed to clear credential", nil)
				return
			}
			continue
		}
		if _, err := store.Put(ctx, tenantID, key, secrets.PutInput{
			Value:       value,
			Description: "Integration credential " + slot + "/" + field,
		}); err != nil {
			if errors.Is(err, secrets.ErrInvalidKey) {
				writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
					"invalid credential field", nil)
				return
			}
			s.log.Error("integrations: put failed",
				"slot", slot, "field", field, "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL",
				"failed to store credential", nil)
			return
		}
	}

	st, err := s.buildSlotStatus(ctx, tenantID, slot, store)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL",
			"failed to read integration status", nil)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ─── registration (called from server.go) ─────────────────────────────────

func (s *Server) registerAdminIntegrationRoutes() {
	// Operator-gated: credential status + writes must never be readable or
	// mutable without an operator session. Reuses the adapter RBAC resource
	// since integrations are adapter credentials.
	s.mux.HandleFunc("GET /api/v1/admin/integrations",
		s.operatorGuard(rbac.ResourceAdminAdapters, s.handleAdminListIntegrations))
	s.mux.HandleFunc("PUT /api/v1/admin/integrations/{slot}",
		s.operatorGuard(rbac.ResourceAdminAdapters, s.handleAdminPutIntegration))
}

func (s *Server) registerAdminIntegrationOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/integrations", openapi.RouteMeta{
		Summary:       "List adapter integration credential status",
		Tags:          []string{"admin"},
		OkDescription: "Per-slot credential status (masked; never raw values)",
	})
	s.openapi.Register("PUT", "/api/v1/admin/integrations/{slot}", openapi.RouteMeta{
		Summary:       "Set adapter integration credentials for a slot",
		Tags:          []string{"admin"},
		OkDescription: "Updated per-slot credential status",
	})
}
