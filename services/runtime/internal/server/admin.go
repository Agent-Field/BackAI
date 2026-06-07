// SPDX-License-Identifier: Apache-2.0

// admin.go — REST handlers for the AF Stack multi-tenancy admin surface.
//
// Endpoints map 1:1 to the admin section in apps/dashboard/src/lib/api.ts.
// Every response shape, every query/path/body field name is snake_case and
// matches the zod schemas there — drift breaks the dashboard's safeParse.
//
// Gating: the multi-tenancy module is OFF by default. When the module
// flag is false, every handler short-circuits with 503
// {error: {code: "MT_DISABLED", ...}}. When the flag is on but no
// tenancy.Manager is wired (Phase 6.1 boot-mode), endpoints return 503
// MT_NOT_CONFIGURED so the dashboard can still render an empty state.
//
// Phase 6.1 owns the tenancy package and wires Server.Deps.Tenancy.
// Phase 6.2 owns the rate limiter + the openapi generator. The
// openapi.Register() calls below are no-ops until that lands.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
)

// ─── Route registration ───────────────────────────────────────────────────

// registerAdminRoutes wires the multi-tenancy admin endpoints. Called
// from registerRoutes() in server.go.
//
// Phase 6.2 owns the openapi registrations — the openapi package now
// uses a Builder pattern (NewBuilder(...).Register(method, path, meta))
// rather than the package-level shim originally planned. Once that
// Builder is plumbed through Deps, each admin route here gets a
// matching b.Register(...) call describing the response component.
func (s *Server) registerAdminRoutes() {
	// Tenants.
	s.mux.HandleFunc("GET /api/v1/admin/tenants", s.handleAdminListTenants)
	s.mux.HandleFunc("POST /api/v1/admin/tenants", s.handleAdminCreateTenant)
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{id}", s.handleAdminGetTenant)
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{id}/drilldown", s.handleAdminGetTenantDrilldown)
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{id}", s.handleAdminUpdateTenant)
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{id}", s.handleAdminDeleteTenant)

	// Users.
	s.mux.HandleFunc("GET /api/v1/admin/users", s.handleAdminListUsers)

	// Memberships.
	s.mux.HandleFunc("GET /api/v1/admin/memberships", s.handleAdminListMemberships)
	s.mux.HandleFunc("POST /api/v1/admin/memberships", s.handleAdminAddMembership)
	s.mux.HandleFunc("DELETE /api/v1/admin/memberships/{tenantId}/{userId}", s.handleAdminRemoveMembership)

	// API keys.
	s.mux.HandleFunc("GET /api/v1/admin/keys", s.handleAdminListKeys)
	s.mux.HandleFunc("POST /api/v1/admin/keys", s.handleAdminIssueKey)
	s.mux.HandleFunc("DELETE /api/v1/admin/keys/{id}", s.handleAdminRevokeKey)

	// Audit log.
	s.mux.HandleFunc("GET /api/v1/admin/audit", s.handleAdminListAudit)
}

// ─── Gating + error helpers ───────────────────────────────────────────────

// multiTenancyEnabled returns true when the multi-tenancy module flag
// is on. Mirrors the resolution in dashboard.go's handleModulesState so
// admin gating and the GET /api/v1/modules response can never disagree.
func (s *Server) multiTenancyEnabled() bool {
	cfg := s.cfg.Modules.Enabled
	if cfg == nil {
		return false
	}
	if v, ok := cfg["multi-tenancy"]; ok {
		return v
	}
	// Default per v1ModuleCatalogue: multi-tenancy is OFF unless pinned.
	return false
}

// adminUnavailable enforces the module gate. Returns true (and writes a
// 503 envelope) if the caller should NOT proceed.
//
//   - module flag off          -> 503 MT_DISABLED
//   - module on, manager nil   -> 503 MT_NOT_CONFIGURED
func (s *Server) adminUnavailable(w http.ResponseWriter) bool {
	if !s.multiTenancyEnabled() {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("MT_DISABLED", "multi-tenancy module not enabled"))
		return true
	}
	if s.tenancy == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("MT_NOT_CONFIGURED", "multi-tenancy manager not configured"))
		return true
	}
	return false
}

// writeTenancyError maps tenancy package sentinel errors to HTTP.
func writeTenancyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tenancy.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errEnvelope("NOT_FOUND", err.Error()))
	case errors.Is(err, tenancy.ErrConflict):
		writeJSON(w, http.StatusConflict, errEnvelope("CONFLICT", err.Error()))
	case errors.Is(err, tenancy.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", err.Error()))
	default:
		writeJSON(w, http.StatusInternalServerError, errEnvelope("INTERNAL", "tenancy operation failed"))
	}
}

// readJSONBody decodes a small JSON body (1 MiB cap) into dst. On error
// it writes a clean envelope to w and returns false.
func readJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("BAD_REQUEST", "could not read body"))
		return false
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "request body is required"))
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "invalid JSON body: "+err.Error()))
		return false
	}
	return true
}

// ─── Wire shapes (mirror apps/dashboard/src/lib/api.ts) ───────────────────

// tenantWire mirrors TenantSchema.
type tenantWire struct {
	ID        string                 `json:"id"`
	Slug      string                 `json:"slug"`
	Name      string                 `json:"name"`
	Plan      string                 `json:"plan"`
	Settings  map[string]interface{} `json:"settings"`
	Quota     map[string]interface{} `json:"quota"`
	CreatedAt string                 `json:"created_at"`
	DeletedAt *string                `json:"deleted_at"`
}

// tenantListWire mirrors TenantListSchema.
type tenantListWire struct {
	Tenants []tenantWire `json:"tenants"`
}

// userWire mirrors UserSchema.
type userWire struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      *string `json:"name"`
	AvatarURL *string `json:"avatar_url"`
	CreatedAt string  `json:"created_at"`
	DeletedAt *string `json:"deleted_at"`
}

type userListWire struct {
	Users []userWire `json:"users"`
}

// membershipWire mirrors MembershipSchema.
type membershipWire struct {
	TenantID   string  `json:"tenant_id"`
	UserID     string  `json:"user_id"`
	Role       string  `json:"role"`
	InvitedAt  string  `json:"invited_at"`
	AcceptedAt *string `json:"accepted_at"`
}

type membershipListWire struct {
	Memberships []membershipWire `json:"memberships"`
}

// apiKeyWire mirrors APIKeySchema (no plaintext).
type apiKeyWire struct {
	ID         string   `json:"id"`
	TenantID   string   `json:"tenant_id"`
	Prefix     string   `json:"prefix"`
	Name       *string  `json:"name"`
	Scopes     []string `json:"scopes"`
	CreatedBy  *string  `json:"created_by"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at"`
	ExpiresAt  *string  `json:"expires_at"`
	RevokedAt  *string  `json:"revoked_at"`
}

type apiKeyListWire struct {
	Keys []apiKeyWire `json:"keys"`
}

// issuedKeyWire mirrors IssuedAPIKeySchema (APIKey + plaintext value).
// Returned ONCE by POST /api/v1/admin/keys.
type issuedKeyWire struct {
	apiKeyWire
	Value string `json:"value"`
}

// tenantDetailMemberWire mirrors TenantDetailSchema.members[].
type tenantDetailMemberWire struct {
	User userWire `json:"user"`
	Role string   `json:"role"`
}

// tenantDetailUsageWire mirrors TenantDetailSchema.usage.
type tenantDetailUsageWire struct {
	Requests30D  int64   `json:"requests_30d"`
	CostUSD30D   float64 `json:"cost_usd_30d"`
	StorageBytes int64   `json:"storage_bytes"`
	SecretsCount int     `json:"secrets_count"`
}

// tenantDetailWire mirrors TenantDetailSchema.
type tenantDetailWire struct {
	Tenant  tenantWire               `json:"tenant"`
	Members []tenantDetailMemberWire `json:"members"`
	APIKeys []apiKeyWire             `json:"api_keys"`
	Usage   tenantDetailUsageWire    `json:"usage"`
}

// ─── Phase 12.1 — tenant drilldown wire shapes ────────────────────────────
//
// Mirrors TenantDrilldownSchema in apps/dashboard/src/lib/api.ts. The
// schema is a strict superset of TenantDetail: members carry
// last_active_at, usage carries 24-bucket sparklines, and the payload
// also includes recent runs, recent webhook deliveries, and a nullable
// billing snapshot.

// tenantDrilldownMemberWire mirrors TenantDrilldownSchema.members[].
type tenantDrilldownMemberWire struct {
	User         userWire `json:"user"`
	Role         string   `json:"role"`
	LastActiveAt *string  `json:"last_active_at"`
}

// tenantDrilldownUsageWire mirrors TenantDrilldownSchema.usage.
type tenantDrilldownUsageWire struct {
	Requests30D      int64     `json:"requests_30d"`
	CostUSD30D       float64   `json:"cost_usd_30d"`
	StorageBytes     int64     `json:"storage_bytes"`
	SecretsCount     int       `json:"secrets_count"`
	CostSparkline    []float64 `json:"cost_sparkline"`
	RequestSparkline []float64 `json:"request_sparkline"`
}

// tenantDrilldownRunWire mirrors TenantDrilldownSchema.recent_runs[].
type tenantDrilldownRunWire struct {
	ID         string  `json:"id"`
	Agent      string  `json:"agent"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	DurationMS int64   `json:"duration_ms"`
	CostUSD    float64 `json:"cost_usd"`
}

// tenantDrilldownWebhookWire mirrors TenantDrilldownSchema.recent_webhooks[].
type tenantDrilldownWebhookWire struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	EventType string `json:"event_type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// tenantDrilldownBillingWire mirrors TenantDrilldownSchema.billing.
// Pointer fields serialise as JSON null when unset, matching zod
// .nullable() expectations.
type tenantDrilldownBillingWire struct {
	Plan               string  `json:"plan"`
	SubscriptionStatus *string `json:"subscription_status"`
	CurrentPeriodEnd   *string `json:"current_period_end"`
	TrialEndsAt        *string `json:"trial_ends_at"`
}

// tenantDrilldownWire mirrors TenantDrilldownSchema.
type tenantDrilldownWire struct {
	Tenant         tenantWire                   `json:"tenant"`
	Members        []tenantDrilldownMemberWire  `json:"members"`
	APIKeys        []apiKeyWire                 `json:"api_keys"`
	Usage          tenantDrilldownUsageWire     `json:"usage"`
	RecentRuns     []tenantDrilldownRunWire     `json:"recent_runs"`
	RecentWebhooks []tenantDrilldownWebhookWire `json:"recent_webhooks"`
	Billing        *tenantDrilldownBillingWire  `json:"billing"`
}

// auditEntryWire mirrors AuditEntrySchema.
type auditEntryWire struct {
	ID           string                 `json:"id"`
	TenantID     *string                `json:"tenant_id"`
	UserID       *string                `json:"user_id"`
	APIKeyID     *string                `json:"api_key_id"`
	Action       string                 `json:"action"`
	ResourceType *string                `json:"resource_type"`
	ResourceID   *string                `json:"resource_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	OccurredAt   string                 `json:"occurred_at"`
}

// auditListWire mirrors AuditListSchema.
type auditListWire struct {
	Entries []auditEntryWire `json:"entries"`
	Total   int              `json:"total"`
	HasMore bool             `json:"has_more"`
}

// createTenantInputWire mirrors CreateTenantInputSchema.
type createTenantInputWire struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Plan string `json:"plan,omitempty"`
}

// updateTenantInputWire mirrors UpdateTenantInputSchema.
type updateTenantInputWire struct {
	Name     *string                `json:"name,omitempty"`
	Plan     *string                `json:"plan,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
	Quota    map[string]interface{} `json:"quota,omitempty"`
}

// issueAPIKeyInputWire mirrors IssueAPIKeyInputSchema.
type issueAPIKeyInputWire struct {
	TenantID  string   `json:"tenant_id"`
	Name      string   `json:"name,omitempty"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at,omitempty"`
}

// addMembershipInputWire is the body for POST /api/v1/admin/memberships.
type addMembershipInputWire struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
}

// ─── Marshallers ──────────────────────────────────────────────────────────

func marshalTenant(t tenancy.Tenant) tenantWire {
	settings := t.Settings
	if settings == nil {
		settings = map[string]interface{}{}
	}
	quota := t.Quota
	if quota == nil {
		quota = map[string]interface{}{}
	}
	w := tenantWire{
		ID:        t.ID,
		Slug:      t.Slug,
		Name:      t.Name,
		Plan:      t.Plan,
		Settings:  settings,
		Quota:     quota,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if t.DeletedAt != nil {
		s := t.DeletedAt.UTC().Format(time.RFC3339Nano)
		w.DeletedAt = &s
	}
	return w
}

func marshalUser(u tenancy.User) userWire {
	w := userWire{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		AvatarURL: u.AvatarURL,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if u.DeletedAt != nil {
		s := u.DeletedAt.UTC().Format(time.RFC3339Nano)
		w.DeletedAt = &s
	}
	return w
}

func marshalMembership(m tenancy.Membership) membershipWire {
	w := membershipWire{
		TenantID:  m.TenantID,
		UserID:    m.UserID,
		Role:      m.Role,
		InvitedAt: m.InvitedAt.UTC().Format(time.RFC3339Nano),
	}
	if m.AcceptedAt != nil {
		s := m.AcceptedAt.UTC().Format(time.RFC3339Nano)
		w.AcceptedAt = &s
	}
	return w
}

func marshalAPIKey(k tenancy.APIKey) apiKeyWire {
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	w := apiKeyWire{
		ID:        k.ID,
		TenantID:  k.TenantID,
		Prefix:    k.Prefix,
		Name:      k.Name,
		Scopes:    scopes,
		CreatedBy: k.CreatedBy,
		CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if k.LastUsedAt != nil {
		s := k.LastUsedAt.UTC().Format(time.RFC3339Nano)
		w.LastUsedAt = &s
	}
	if k.ExpiresAt != nil {
		s := k.ExpiresAt.UTC().Format(time.RFC3339Nano)
		w.ExpiresAt = &s
	}
	if k.RevokedAt != nil {
		s := k.RevokedAt.UTC().Format(time.RFC3339Nano)
		w.RevokedAt = &s
	}
	return w
}

func marshalIssuedKey(k tenancy.IssuedAPIKey) issuedKeyWire {
	return issuedKeyWire{
		apiKeyWire: marshalAPIKey(k.APIKey),
		Value:      k.Value,
	}
}

func marshalAuditEntry(e tenancy.AuditEntry) auditEntryWire {
	meta := e.Metadata
	if meta == nil {
		meta = map[string]interface{}{}
	}
	return auditEntryWire{
		ID:           e.ID,
		TenantID:     e.TenantID,
		UserID:       e.UserID,
		APIKeyID:     e.APIKeyID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Metadata:     meta,
		OccurredAt:   e.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// marshalTenantDrilldown converts the tenancy-layer aggregate into its
// wire shape. Keeps every nullable field as a JSON-null pointer so the
// dashboard's zod safeParse never sees `undefined` where it expects
// `null`. Sparkline arrays are always 24-long (the tenancy layer
// guarantees zeroed defaults).
func marshalTenantDrilldown(d tenancy.TenantDrilldown) tenantDrilldownWire {
	members := make([]tenantDrilldownMemberWire, 0, len(d.Members))
	for _, m := range d.Members {
		mw := tenantDrilldownMemberWire{
			User: marshalUser(m.User),
			Role: m.Role,
		}
		if m.LastActiveAt != nil {
			s := m.LastActiveAt.UTC().Format(time.RFC3339Nano)
			mw.LastActiveAt = &s
		}
		members = append(members, mw)
	}
	keys := make([]apiKeyWire, 0, len(d.APIKeys))
	for _, k := range d.APIKeys {
		keys = append(keys, marshalAPIKey(k))
	}
	runs := make([]tenantDrilldownRunWire, 0, len(d.RecentRuns))
	for _, r := range d.RecentRuns {
		runs = append(runs, tenantDrilldownRunWire{
			ID:         r.ID,
			Agent:      r.Agent,
			Status:     r.Status,
			StartedAt:  r.StartedAt.UTC().Format(time.RFC3339Nano),
			DurationMS: r.DurationMS,
			CostUSD:    r.CostUSD,
		})
	}
	hooks := make([]tenantDrilldownWebhookWire, 0, len(d.RecentWebhooks))
	for _, h := range d.RecentWebhooks {
		hooks = append(hooks, tenantDrilldownWebhookWire{
			ID:        h.ID,
			Direction: h.Direction,
			EventType: h.EventType,
			Status:    h.Status,
			CreatedAt: h.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	costSpark := d.Usage.CostSparkline
	if len(costSpark) != 24 {
		costSpark = make([]float64, 24)
	}
	reqSpark := d.Usage.RequestSparkline
	if len(reqSpark) != 24 {
		reqSpark = make([]float64, 24)
	}
	var billing *tenantDrilldownBillingWire
	if d.Billing != nil {
		bw := &tenantDrilldownBillingWire{
			Plan: d.Billing.Plan,
		}
		if d.Billing.SubscriptionStatus != nil {
			s := *d.Billing.SubscriptionStatus
			bw.SubscriptionStatus = &s
		}
		if d.Billing.CurrentPeriodEnd != nil {
			s := d.Billing.CurrentPeriodEnd.UTC().Format(time.RFC3339Nano)
			bw.CurrentPeriodEnd = &s
		}
		if d.Billing.TrialEndsAt != nil {
			s := d.Billing.TrialEndsAt.UTC().Format(time.RFC3339Nano)
			bw.TrialEndsAt = &s
		}
		billing = bw
	}
	return tenantDrilldownWire{
		Tenant:  marshalTenant(d.Tenant),
		Members: members,
		APIKeys: keys,
		Usage: tenantDrilldownUsageWire{
			Requests30D:      d.Usage.Requests30D,
			CostUSD30D:       d.Usage.CostUSD30D,
			StorageBytes:     d.Usage.StorageBytes,
			SecretsCount:     d.Usage.SecretsCount,
			CostSparkline:    costSpark,
			RequestSparkline: reqSpark,
		},
		RecentRuns:     runs,
		RecentWebhooks: hooks,
		Billing:        billing,
	}
}

func marshalTenantDetail(d tenancy.TenantDetail) tenantDetailWire {
	members := make([]tenantDetailMemberWire, 0, len(d.Members))
	for _, m := range d.Members {
		members = append(members, tenantDetailMemberWire{
			User: marshalUser(m.User),
			Role: m.Role,
		})
	}
	keys := make([]apiKeyWire, 0, len(d.APIKeys))
	for _, k := range d.APIKeys {
		keys = append(keys, marshalAPIKey(k))
	}
	return tenantDetailWire{
		Tenant:  marshalTenant(d.Tenant),
		Members: members,
		APIKeys: keys,
		Usage: tenantDetailUsageWire{
			Requests30D:  d.Usage.Requests30D,
			CostUSD30D:   d.Usage.CostUSD30D,
			StorageBytes: d.Usage.StorageBytes,
			SecretsCount: d.Usage.SecretsCount,
		},
	}
}

// ─── Tenants ──────────────────────────────────────────────────────────────

func (s *Server) handleAdminListTenants(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.list")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	rows, err := s.tenancy.ListTenants(ctx)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	out := make([]tenantWire, 0, len(rows))
	for _, t := range rows {
		out = append(out, marshalTenant(t))
	}
	writeJSON(w, http.StatusOK, tenantListWire{Tenants: out})
}

func (s *Server) handleAdminCreateTenant(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.create")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	var in createTenantInputWire
	if !readJSONBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Slug) == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "slug is required"))
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "name is required"))
		return
	}
	span.SetAttributes(attribute.String("tenant.slug", in.Slug))
	t, err := s.tenancy.CreateTenant(ctx, tenancy.CreateTenantInput{
		Slug: in.Slug,
		Name: in.Name,
		Plan: in.Plan,
	})
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "tenant.create",
		ResourceType: "tenant",
		ResourceID:   t.ID,
		Metadata: map[string]any{
			"slug": t.Slug,
			"name": t.Name,
			"plan": t.Plan,
		},
	})
	writeJSON(w, http.StatusCreated, marshalTenant(t))
}

func (s *Server) handleAdminGetTenant(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.get")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "tenant id required"))
		return
	}
	span.SetAttributes(attribute.String("tenant.id", id))
	detail, err := s.tenancy.GetTenant(ctx, id)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marshalTenantDetail(detail))
}

// handleAdminGetTenantDrilldown is Phase 12.1's "everything about this
// tenant" endpoint. Backs the dashboard's /customers/tenants/[id] page
// and is a strict superset of GetTenant — members carry last_active_at,
// usage carries 24-bucket sparklines, and recent runs / webhooks /
// billing snapshot are folded in.
func (s *Server) handleAdminGetTenantDrilldown(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.drilldown")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "tenant id required"))
		return
	}
	span.SetAttributes(attribute.String("tenant.id", id))
	drilldown, err := s.tenancy.GetTenantDrilldown(ctx, id)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marshalTenantDrilldown(drilldown))
}

func (s *Server) handleAdminUpdateTenant(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.update")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "tenant id required"))
		return
	}
	var in updateTenantInputWire
	if !readJSONBody(w, r, &in) {
		return
	}
	span.SetAttributes(attribute.String("tenant.id", id))
	t, err := s.tenancy.UpdateTenant(ctx, id, tenancy.UpdateTenantInput{
		Name:     in.Name,
		Plan:     in.Plan,
		Settings: in.Settings,
		Quota:    in.Quota,
	})
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, marshalTenant(t))
}

func (s *Server) handleAdminDeleteTenant(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.tenants.delete")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "tenant id required"))
		return
	}
	if err := s.tenancy.DeleteTenant(ctx, id); err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "tenant.delete",
		ResourceType: "tenant",
		ResourceID:   id,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── Users ────────────────────────────────────────────────────────────────

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.users.list")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	q := r.URL.Query()
	tenantID := strings.TrimSpace(q.Get("tenant"))
	search := strings.TrimSpace(q.Get("search"))
	span.SetAttributes(
		attribute.String("filter.tenant", tenantID),
		attribute.String("filter.search", search),
	)
	users, err := s.tenancy.ListUsers(ctx, tenantID, search)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	out := make([]userWire, 0, len(users))
	for _, u := range users {
		out = append(out, marshalUser(u))
	}
	writeJSON(w, http.StatusOK, userListWire{Users: out})
}

// ─── Memberships ──────────────────────────────────────────────────────────

func (s *Server) handleAdminListMemberships(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.memberships.list")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	q := r.URL.Query()
	tenantID := strings.TrimSpace(q.Get("tenant"))
	userID := strings.TrimSpace(q.Get("user"))
	rows, err := s.tenancy.ListMemberships(ctx, tenantID, userID)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	out := make([]membershipWire, 0, len(rows))
	for _, m := range rows {
		out = append(out, marshalMembership(m))
	}
	writeJSON(w, http.StatusOK, membershipListWire{Memberships: out})
}

func (s *Server) handleAdminAddMembership(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.memberships.add")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	var in addMembershipInputWire
	if !readJSONBody(w, r, &in) {
		return
	}
	if in.TenantID == "" || in.UserID == "" || in.Role == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "tenant_id, user_id, role are required"))
		return
	}
	m, err := s.tenancy.AddMembership(ctx, in.TenantID, in.UserID, in.Role)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "membership.add",
		ResourceType: "membership",
		ResourceID:   in.TenantID + "/" + in.UserID,
		Metadata: map[string]any{
			"tenant_id": in.TenantID,
			"user_id":   in.UserID,
			"role":      in.Role,
		},
	})
	writeJSON(w, http.StatusCreated, marshalMembership(m))
}

func (s *Server) handleAdminRemoveMembership(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.memberships.remove")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if tenantID == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "tenantId and userId are required"))
		return
	}
	if err := s.tenancy.RemoveMembership(ctx, tenantID, userID); err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "membership.remove",
		ResourceType: "membership",
		ResourceID:   tenantID + "/" + userID,
		Metadata: map[string]any{
			"tenant_id": tenantID,
			"user_id":   userID,
		},
	})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── API keys ─────────────────────────────────────────────────────────────

func (s *Server) handleAdminListKeys(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.keys.list")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant"))
	rows, err := s.tenancy.ListAPIKeys(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	out := make([]apiKeyWire, 0, len(rows))
	for _, k := range rows {
		out = append(out, marshalAPIKey(k))
	}
	writeJSON(w, http.StatusOK, apiKeyListWire{Keys: out})
}

func (s *Server) handleAdminIssueKey(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.keys.issue")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	var in issueAPIKeyInputWire
	if !readJSONBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.TenantID) == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "tenant_id is required"))
		return
	}
	scopes := in.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	issue := tenancy.IssueAPIKeyInput{
		TenantID: in.TenantID,
		Name:     in.Name,
		Scopes:   scopes,
	}
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := parseRFC3339(*in.ExpiresAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest,
				errEnvelope("VALIDATION_FAILED", "expires_at must be RFC3339"))
			return
		}
		issue.ExpiresAt = &t
	}
	issued, err := s.tenancy.IssueAPIKey(ctx, issue)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "api_key.create",
		ResourceType: "api_key",
		ResourceID:   issued.ID,
		Metadata: map[string]any{
			"tenant_id": issued.TenantID,
			"name":      issued.Name,
			"scopes":    issued.Scopes,
		},
	})
	// The plaintext leaves the runtime here ONCE. Subsequent GETs only
	// see the APIKey shape (no `value`).
	writeJSON(w, http.StatusCreated, marshalIssuedKey(issued))
}

func (s *Server) handleAdminRevokeKey(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.keys.revoke")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errEnvelope("VALIDATION_FAILED", "key id required"))
		return
	}
	if err := s.tenancy.RevokeAPIKey(ctx, id); err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(ctx, r, audit.Event{
		Action:       "api_key.revoke",
		ResourceType: "api_key",
		ResourceID:   id,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// ─── Audit ────────────────────────────────────────────────────────────────

func (s *Server) handleAdminListAudit(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "admin.audit.list")
	defer span.End()
	if s.adminUnavailable(w) {
		return
	}
	q := r.URL.Query()
	limit, offset := parsePaging(q.Get("limit"), q.Get("offset"))
	filter := tenancy.AuditFilter{
		TenantID: strings.TrimSpace(q.Get("tenant")),
		Action:   strings.TrimSpace(q.Get("action")),
		Limit:    limit,
		Offset:   offset,
	}
	if v := strings.TrimSpace(q.Get("from")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest,
				errEnvelope("VALIDATION_FAILED", "from must be RFC3339"))
			return
		}
		filter.From = &t
	}
	if v := strings.TrimSpace(q.Get("to")); v != "" {
		t, err := parseRFC3339(v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest,
				errEnvelope("VALIDATION_FAILED", "to must be RFC3339"))
			return
		}
		filter.To = &t
	}
	span.SetAttributes(
		attribute.Int("paging.limit", limit),
		attribute.Int("paging.offset", offset),
		attribute.String("filter.tenant", filter.TenantID),
		attribute.String("filter.action", filter.Action),
	)
	page, err := s.tenancy.ListAudit(ctx, filter)
	if err != nil {
		span.RecordError(err)
		writeTenancyError(w, err)
		return
	}
	entries := make([]auditEntryWire, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, marshalAuditEntry(e))
	}
	writeJSON(w, http.StatusOK, auditListWire{
		Entries: entries,
		Total:   page.Total,
		HasMore: page.HasMore,
	})
}

// ─── OpenAPI registrations ────────────────────────────────────────────────

// registerAdminOpenAPI publishes the admin routes into the live OpenAPI
// 3.1 builder. Tolerates a nil builder (test path).
//
// Lives in admin.go (not openapi_routes.go) so future param/response
// metadata changes land in the same file as the handler they describe.
func (s *Server) registerAdminOpenAPI() {
	if s.openapi == nil {
		return
	}
	b := s.openapi
	tags := []string{"admin"}
	// Tenants.
	b.Register("GET", "/api/v1/admin/tenants", openapi.RouteMeta{Summary: "List tenants", Tags: tags})
	b.Register("POST", "/api/v1/admin/tenants", openapi.RouteMeta{Summary: "Create a tenant", Tags: tags})
	b.Register("GET", "/api/v1/admin/tenants/{id}", openapi.RouteMeta{Summary: "Get tenant detail (members, keys, usage)", Tags: tags})
	b.Register("GET", "/api/v1/admin/tenants/{id}/drilldown", openapi.RouteMeta{Summary: "Get full per-tenant drilldown (Phase 12.1: members + keys + usage + sparklines + recent runs + recent webhooks + billing)", Tags: tags})
	b.Register("PATCH", "/api/v1/admin/tenants/{id}", openapi.RouteMeta{Summary: "Update a tenant", Tags: tags})
	b.Register("DELETE", "/api/v1/admin/tenants/{id}", openapi.RouteMeta{Summary: "Soft-delete a tenant", Tags: tags})
	// Users.
	b.Register("GET", "/api/v1/admin/users", openapi.RouteMeta{Summary: "List users", Tags: tags})
	// Memberships.
	b.Register("GET", "/api/v1/admin/memberships", openapi.RouteMeta{Summary: "List memberships", Tags: tags})
	b.Register("POST", "/api/v1/admin/memberships", openapi.RouteMeta{Summary: "Add a tenant member", Tags: tags})
	b.Register("DELETE", "/api/v1/admin/memberships/{tenantId}/{userId}", openapi.RouteMeta{Summary: "Remove a tenant member", Tags: tags})
	// API keys.
	b.Register("GET", "/api/v1/admin/keys", openapi.RouteMeta{Summary: "List API keys", Tags: tags})
	b.Register("POST", "/api/v1/admin/keys", openapi.RouteMeta{Summary: "Issue a new API key (plaintext value returned ONCE)", Tags: tags})
	b.Register("DELETE", "/api/v1/admin/keys/{id}", openapi.RouteMeta{Summary: "Revoke an API key", Tags: tags})
	// Audit.
	b.Register("GET", "/api/v1/admin/audit", openapi.RouteMeta{Summary: "List audit entries", Tags: tags})
}

// Compile-time assurance: strconv import is used by parsePaging via the
// path-shared helper in dashboard.go; we import the package here so a
// future refactor that moves parsePaging here still compiles.
var _ = strconv.Itoa